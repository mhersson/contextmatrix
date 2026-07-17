// Package images provides image processing, content-hash deduplication, and
// SQLite-backed persistence for uploaded screenshots and images. The package is
// safe for concurrent use. To bound peak memory under concurrent uploads,
// decode+encode work is gated by a package-level semaphore (processSem) capped
// at maxConcurrentDecodes goroutines. Each slot may allocate up to ~128 MiB of
// RGBA pixel data (maxPixels × 4 bytes), so the worst-case resident increase is
// maxConcurrentDecodes × ~128 MiB. Callers that exceed the cap block until a
// slot becomes available - they are not rejected.
package images

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"net/http"
	"runtime"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// MaxUploadBytes caps the raw-byte size of any image accepted by Process and,
// transitively, by the upload handler and middleware. Exported so the HTTP
// layer can size its own caps off the same source of truth - the user-facing
// "exceeds N MB" message and the bodyLimit envelope must never drift from this.
const MaxUploadBytes = 10 << 20 // 10 MiB

// maxWidth and maxHeight define the bounding box for resizing.
const (
	maxWidth  = 1024
	maxHeight = 768
)

// maxPixels caps the *decoded* image area before we materialize an RGBA
// buffer. A highly-compressed PNG (e.g. solid colour) can decode to enormous
// dimensions and trigger a ~4*W*H byte allocation that OOM-kills the server
// - the so-called "decompression bomb" pattern. 32 megapixels (≈5800x5800)
// peaks at ~128 MB RGBA per image, giving defense-in-depth against
// concurrent decode storms while still admitting any legitimate cropped
// screenshot (the resize step caps egress at 1024x768 regardless).
const maxPixels int64 = 32 << 20

// maxDim caps either dimension independently. Defense in depth on top of
// maxPixels: a tall-thin or wide-flat image with W*H under maxPixels but a
// single dimension in the tens of thousands still stresses decoder paths and
// downstream resize buffers, and bounding each dimension also eliminates any
// concern about 32-bit integer overflow inside W*H math.
const maxDim = 16384

// maxConcurrentDecodes is the maximum number of goroutines that may be inside
// the decode+encode section of Process simultaneously. At maxPixels each slot
// may allocate ~128 MiB of RGBA data; capping at GOMAXPROCS (floored at 2,
// ceiling at 8) keeps peak decode memory proportional to available CPUs without
// starving single-CPU deployments.
var maxConcurrentDecodes = func() int {
	n := min(max(runtime.GOMAXPROCS(0), 2), 8)

	return n
}()

// processSem is a counting semaphore that limits concurrent decode+encode work
// inside Process. Acquire by sending, release by receiving.
var processSem = make(chan struct{}, maxConcurrentDecodes)

// Process validates, optionally resizes, and re-encodes raw image bytes.
// Supported input formats: image/png, image/jpeg, image/gif (single-frame),
// image/webp. The output format follows the input except that single-frame
// GIFs and WebP images are re-encoded as PNG.
//
// At most maxConcurrentDecodes calls may be in the decode+encode phase
// concurrently; excess callers block until a slot is free.
//
// Errors returned: ErrTooLarge, ErrUnsupportedFormat, ErrAnimated.
func Process(raw []byte) (processed []byte, contentType string, err error) {
	if len(raw) > MaxUploadBytes {
		return nil, "", ErrTooLarge
	}

	ct := http.DetectContentType(raw)

	// Reject unsupported formats before acquiring the semaphore so the cheap
	// format check never queues behind decode work.
	switch ct {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return nil, "", ErrUnsupportedFormat
	}

	// Acquire a decode slot. Blocks when maxConcurrentDecodes goroutines are
	// already inside the decode+encode path. Released after encode completes.
	processSem <- struct{}{}

	defer func() { <-processSem }()

	switch ct {
	case "image/png":
		return processStdlib(raw, ct, decodePNG, encodePNG)
	case "image/jpeg":
		return processStdlib(raw, ct, decodeJPEG, encodeJPEG)
	case "image/gif":
		return processGIF(raw)
	case "image/webp":
		return processWebP(raw)
	default:
		return nil, "", ErrUnsupportedFormat
	}
}

type (
	decoder func([]byte) (image.Image, error)
	encoder func(*bytes.Buffer, image.Image) error
)

// validateDims rejects implausible header dimensions before any full decoder
// allocates a pixel buffer. Zero-or-negative dimensions are treated as a
// format problem (corrupt/invalid header); oversized dimensions are
// ErrTooLarge. int64 multiplication keeps the area check overflow-safe on
// 32-bit platforms where int is int32.
func validateDims(width, height int) error {
	if width <= 0 || height <= 0 {
		return ErrUnsupportedFormat
	}

	if width > maxDim || height > maxDim {
		return ErrTooLarge
	}

	if int64(width)*int64(height) > maxPixels {
		return ErrTooLarge
	}

	return nil
}

func processStdlib(raw []byte, ct string, dec decoder, enc encoder) ([]byte, string, error) {
	// DecodeConfig reads only the header - verify the format and bound the
	// pixel area before we let the full decoder allocate a backing buffer.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", ErrUnsupportedFormat
	}

	if err := validateDims(cfg.Width, cfg.Height); err != nil {
		return nil, "", err
	}

	img, err := dec(raw)
	if err != nil {
		// Header was acceptable but the full decoder rejected the body.
		// That is still a malformed-content problem, not a server-internal
		// one - map to ErrUnsupportedFormat (handler → 415) for parity
		// with the GIF path. Underlying cause is preserved in logs.
		slog.Debug("images: decode failed after dim check",
			"content_type", ct, "error", err)

		return nil, "", ErrUnsupportedFormat
	}

	img = maybeResize(img)

	var buf bytes.Buffer
	if err := enc(&buf, img); err != nil {
		return nil, "", fmt.Errorf("images: encode: %w", err)
	}

	return buf.Bytes(), ct, nil
}

func processGIF(raw []byte) ([]byte, string, error) {
	cfg, err := gif.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", ErrUnsupportedFormat
	}

	if err := validateDims(cfg.Width, cfg.Height); err != nil {
		return nil, "", err
	}

	// Reject multi-frame GIFs *before* gif.DecodeAll: each frame decodes to a
	// W*H *image.Paletted buffer, so a tiny encoded multi-frame GIF
	// (LZW-compressed solid colour, well under MaxUploadBytes) can otherwise
	// allocate gigabytes of resident memory before the post-decode
	// `len(g.Image) > 1` check fires. The header walker counts image
	// descriptors without decompressing pixel data.
	multi, err := gifHasMultipleFrames(raw)
	if err != nil {
		// The walker rejected the byte sequence as malformed; surface the
		// underlying reason via slog so an operator chasing a "my legitimate
		// GIF won't upload" report can correlate without needing the bytes.
		slog.Debug("images: gif header walker rejected payload", "error", err)

		return nil, "", ErrUnsupportedFormat
	}

	if multi {
		return nil, "", ErrAnimated
	}

	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		// Walker passed but the full decoder rejected the stream - that is
		// still a malformed-content problem, not a server-internal one. Map
		// to ErrUnsupportedFormat (handler → 415) while keeping the cause in
		// the structured log for triage.
		slog.Debug("images: gif decode failed after walker", "error", err)

		return nil, "", ErrUnsupportedFormat
	}

	if len(g.Image) == 0 {
		return nil, "", ErrUnsupportedFormat
	}

	// Single-frame GIF → PNG.
	img := maybeResize(g.Image[0])

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("images: encode gif as png: %w", err)
	}

	return buf.Bytes(), "image/png", nil
}

func processWebP(raw []byte) ([]byte, string, error) {
	cfg, err := webp.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", ErrUnsupportedFormat
	}

	if err := validateDims(cfg.Width, cfg.Height); err != nil {
		return nil, "", err
	}

	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		// Match the GIF/PNG/JPEG behaviour: a body that fails the full
		// decoder is a 415, not a 500. Cause kept in Debug logs.
		slog.Debug("images: webp decode failed after dim check", "error", err)

		return nil, "", ErrUnsupportedFormat
	}

	img = maybeResize(img)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("images: encode webp as png: %w", err)
	}

	return buf.Bytes(), "image/png", nil
}

// gifHasMultipleFrames walks the GIF block structure to count image
// descriptors without decompressing LZW pixel data. Returns true on the
// second image descriptor; bails out (error) on a malformed structure so
// the caller falls through to the regular decoder's error path.
//
// GIF89a spec layout: header(6) + LSD(7) + optional GCT + repeated blocks
// terminated by 0x3B. Blocks are extension introducer (0x21) followed by
// a label and sub-blocks, or image descriptor (0x2C) followed by a 9-byte
// descriptor, optional LCT, LZW min code size, and LZW data sub-blocks.
// Sub-blocks are 1-byte length + N bytes, terminated by a zero-length block.
func gifHasMultipleFrames(raw []byte) (bool, error) {
	const (
		headerLen        = 6
		logicalScreenLen = 7
		imageDescBodyLen = 9
		gctFlag          = 0x80
		extensionByte    = 0x21
		imageDescByte    = 0x2C
		trailerByte      = 0x3B
	)

	if len(raw) < headerLen+logicalScreenLen {
		return false, fmt.Errorf("images: gif too short")
	}

	if string(raw[:3]) != "GIF" {
		return false, fmt.Errorf("images: gif signature missing")
	}

	p := headerLen + logicalScreenLen

	if raw[10]&gctFlag != 0 {
		gctSize := 3 * (1 << ((raw[10] & 0x07) + 1))
		p += gctSize
	}

	frames := 0

	for p < len(raw) {
		b := raw[p]
		p++

		switch b {
		case trailerByte:
			return frames > 1, nil

		case extensionByte:
			if p >= len(raw) {
				return false, fmt.Errorf("images: truncated extension")
			}

			p++ // skip label

			var err error

			p, err = skipSubBlocks(raw, p)
			if err != nil {
				return false, err
			}

		case imageDescByte:
			frames++
			if frames > 1 {
				return true, nil
			}

			if p+imageDescBodyLen > len(raw) {
				return false, fmt.Errorf("images: truncated image descriptor")
			}

			imgPacked := raw[p+8]
			p += imageDescBodyLen

			if imgPacked&gctFlag != 0 {
				lctSize := 3 * (1 << ((imgPacked & 0x07) + 1))
				p += lctSize
			}

			if p >= len(raw) {
				return false, fmt.Errorf("images: missing lzw code size")
			}

			p++ // skip LZW min code size

			var err error

			p, err = skipSubBlocks(raw, p)
			if err != nil {
				return false, err
			}

		default:
			return false, fmt.Errorf("images: unknown block 0x%02x", b)
		}
	}

	return frames > 1, nil
}

// skipSubBlocks advances past a sequence of GIF data sub-blocks (each
// 1-byte length + N bytes), stopping after the terminating zero-length
// block. Returns the new offset.
func skipSubBlocks(raw []byte, p int) (int, error) {
	for p < len(raw) {
		size := int(raw[p])
		p++

		if size == 0 {
			return p, nil
		}

		if p+size > len(raw) {
			return 0, fmt.Errorf("images: truncated sub-block")
		}

		p += size
	}

	return 0, fmt.Errorf("images: unterminated sub-blocks")
}

// maybeResize returns a resized image if either dimension exceeds the bounding
// box, preserving aspect ratio. Returns the original when it already fits.
func maybeResize(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()

	if w <= maxWidth && h <= maxHeight {
		return src
	}

	// Scale to fit within maxWidth x maxHeight, preserving aspect ratio.
	scaleW := float64(maxWidth) / float64(w)
	scaleH := float64(maxHeight) / float64(h)

	scale := scaleW
	if scaleH < scaleW {
		scale = scaleH
	}

	newW := int(float64(w) * scale)
	newH := int(float64(h) * scale)

	// Ensure at least 1 pixel in each dimension.
	if newW < 1 {
		newW = 1
	}

	if newH < 1 {
		newH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, newW, newH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)

	return dst
}

func decodePNG(raw []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(raw))
}

func decodeJPEG(raw []byte) (image.Image, error) {
	return jpeg.Decode(bytes.NewReader(raw))
}

func encodePNG(buf *bytes.Buffer, img image.Image) error {
	return png.Encode(buf, img)
}

func encodeJPEG(buf *bytes.Buffer, img image.Image) error {
	return jpeg.Encode(buf, img, &jpeg.Options{Quality: 85})
}
