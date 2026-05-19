package images

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makePNG encodes a solid-colour image of given dimensions as PNG bytes.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{R: 100, G: 150, B: 200, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// makeJPEG encodes a solid-colour image of given dimensions as JPEG bytes.
func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{R: 200, G: 100, B: 50, A: 255}}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))

	return buf.Bytes()
}

// makeAnimatedGIF creates an animated GIF with numFrames frames.
func makeAnimatedGIF(t *testing.T, numFrames int) []byte {
	t.Helper()

	g := &gif.GIF{}

	for i := range numFrames {
		frame := image.NewPaletted(image.Rect(0, 0, 10, 10), []color.Color{
			color.RGBA{R: uint8(i * 20), G: 0, B: 0, A: 255},
			color.RGBA{R: 0, G: 0, B: 0, A: 255},
		})
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, 10)
	}

	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, g))

	return buf.Bytes()
}

// makeJPEGWithEXIF creates a JPEG with a minimal EXIF APP1 marker embedded.
// The EXIF data is synthetic — just enough to have the marker present.
func makeJPEGWithEXIF(t *testing.T) []byte {
	t.Helper()

	// Build a minimal JPEG: SOI + APP1 (fake EXIF) + real image data.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.White}, image.Point{}, draw.Src)

	var imgBuf bytes.Buffer
	require.NoError(t, jpeg.Encode(&imgBuf, img, &jpeg.Options{Quality: 90}))

	raw := imgBuf.Bytes()

	// Construct a fake APP1 EXIF segment: marker 0xFFE1 + length (2) + data.
	// We insert it after the SOI marker (first 2 bytes: 0xFF 0xD8).
	exifPayload := []byte("Exif\x00\x00II\x2A\x00\x08\x00\x00\x00") // minimal EXIF header
	app1Len := uint16(2 + len(exifPayload))

	var out bytes.Buffer
	out.Write(raw[:2]) // SOI
	out.WriteByte(0xFF)
	out.WriteByte(0xE1) // APP1 marker
	out.WriteByte(byte(app1Len >> 8))
	out.WriteByte(byte(app1Len))
	out.Write(exifPayload)
	out.Write(raw[2:]) // rest of JPEG

	return out.Bytes()
}

func TestProcess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		input           func(t *testing.T) []byte
		wantErr         error
		wantContentType string
		checkOutput     func(t *testing.T, out []byte, ct string)
	}{
		{
			name:            "tiny PNG passthrough (no resize)",
			input:           func(t *testing.T) []byte { return makePNG(t, 100, 100) },
			wantContentType: "image/png",
			checkOutput: func(t *testing.T, out []byte, ct string) {
				t.Helper()

				cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
				require.NoError(t, err)
				assert.Equal(t, 100, cfg.Width)
				assert.Equal(t, 100, cfg.Height)
			},
		},
		{
			name:            "oversized JPEG resized within 1024x768, aspect preserved",
			input:           func(t *testing.T) []byte { return makeJPEG(t, 2000, 2000) },
			wantContentType: "image/jpeg",
			checkOutput: func(t *testing.T, out []byte, ct string) {
				t.Helper()

				cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
				require.NoError(t, err)
				assert.LessOrEqual(t, cfg.Width, maxWidth, "width must not exceed maxWidth")
				assert.LessOrEqual(t, cfg.Height, maxHeight, "height must not exceed maxHeight")
				// For a 2000x2000 input, scale is min(1024/2000, 768/2000) = 768/2000 = 0.384
				// → 768x768
				assert.Equal(t, cfg.Width, cfg.Height, "aspect ratio (1:1) must be preserved")
			},
		},
		{
			name:    "animated GIF rejected with ErrAnimated",
			input:   func(t *testing.T) []byte { return makeAnimatedGIF(t, 3) },
			wantErr: ErrAnimated,
		},
		{
			name: "video bytes (mp4 ftyp magic) rejected with ErrUnsupportedFormat",
			input: func(t *testing.T) []byte {
				// Minimal mp4-like header: 4-byte box size + "ftyp" at offset 4.
				// http.DetectContentType looks at the first 512 bytes.
				raw := make([]byte, 12)
				copy(raw[4:], "ftyp")
				return raw
			},
			wantErr: ErrUnsupportedFormat,
		},
		{
			name: ">10MB blob rejected with ErrTooLarge",
			input: func(t *testing.T) []byte {
				return make([]byte, maxBytes+1)
			},
			wantErr: ErrTooLarge,
		},
		{
			name:            "EXIF-bearing JPEG produces output without Exif marker",
			input:           makeJPEGWithEXIF,
			wantContentType: "image/jpeg",
			checkOutput: func(t *testing.T, out []byte, _ string) {
				t.Helper()

				assert.False(t, strings.Contains(string(out), "Exif\x00\x00"),
					"re-encoded JPEG must not contain EXIF marker")
			},
		},
		{
			name:            "single-frame GIF re-encoded as PNG",
			input:           func(t *testing.T) []byte { return makeAnimatedGIF(t, 1) },
			wantContentType: "image/png",
			checkOutput: func(t *testing.T, out []byte, ct string) {
				t.Helper()

				assert.Equal(t, "image/png", ct)
				// Must decode cleanly as PNG.
				_, err := png.Decode(bytes.NewReader(out))
				require.NoError(t, err)
			},
		},
		{
			// Decompression-bomb guard: a highly-compressed PNG with declared
			// dimensions above maxPixels must be rejected before the full
			// decoder allocates a 4*W*H RGBA backing buffer.
			name: "pixel-bomb PNG rejected before allocation",
			input: func(t *testing.T) []byte {
				// 10000x10000 solid-colour PNG decodes to ~400 MB RGBA but the
				// encoded form is small. We pass under the 10 MB byte cap so
				// the only thing that can reject this is the pixel guard.
				return makePNG(t, 10000, 10000)
			},
			wantErr: ErrTooLarge,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, ct, err := Process(tc.input(t))

			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantContentType, ct)

			if tc.checkOutput != nil {
				tc.checkOutput(t, out, ct)
			}
		})
	}
}
