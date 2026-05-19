package images

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

const maxBytes = 10 << 20 // 10 MB

// maxWidth and maxHeight define the bounding box for resizing.
const (
	maxWidth  = 1024
	maxHeight = 768
)

// maxPixels caps the *decoded* image area before we materialize an RGBA
// buffer. A highly-compressed PNG (e.g. solid colour) can decode to enormous
// dimensions and trigger a ~4*W*H byte allocation that OOM-kills the server
// — the so-called "decompression bomb" pattern. 64 megapixels (≈8000x8000)
// is well above any legitimate screenshot.
const maxPixels = 64 << 20

// Process validates, optionally resizes, and re-encodes raw image bytes.
// Supported input formats: image/png, image/jpeg, image/gif (single-frame),
// image/webp. The output format follows the input except that single-frame
// GIFs and WebP images are re-encoded as PNG.
//
// Errors returned: ErrTooLarge, ErrUnsupportedFormat, ErrAnimated.
func Process(raw []byte) (processed []byte, contentType string, err error) {
	if len(raw) > maxBytes {
		return nil, "", ErrTooLarge
	}

	ct := http.DetectContentType(raw)

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

type decoder func([]byte) (image.Image, error)
type encoder func(*bytes.Buffer, image.Image) error

func processStdlib(raw []byte, ct string, dec decoder, enc encoder) ([]byte, string, error) {
	// DecodeConfig reads only the header — verify the format and bound the
	// pixel area before we let the full decoder allocate a backing buffer.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, "", ErrUnsupportedFormat
	}

	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", ErrTooLarge
	}

	img, err := dec(raw)
	if err != nil {
		return nil, "", fmt.Errorf("images: decode: %w", err)
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

	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", ErrTooLarge
	}

	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("images: decode gif: %w", err)
	}

	if len(g.Image) > 1 {
		return nil, "", ErrAnimated
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

	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", ErrTooLarge
	}

	img, err := webp.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", fmt.Errorf("images: decode webp: %w", err)
	}

	img = maybeResize(img)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("images: encode webp as png: %w", err)
	}

	return buf.Bytes(), "image/png", nil
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
