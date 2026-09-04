package api

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/images"
)

// imageTestServer wires a router with an image store and returns the server URL.
func imageTestServer(t *testing.T) (string, images.Store) {
	t.Helper()

	store, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	router := NewRouter(RouterConfig{ImageStore: store})

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	return srv.URL, store
}

// postMultipart uploads file bytes under field name "file" with the given content type.
func postMultipart(t *testing.T, url string, fieldName, filename, contentType string, data []byte) *http.Response {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="` + fieldName + `"; filename="` + filename + `"`}

	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}

	part, err := writer.CreatePart(header)
	require.NoError(t, err)

	_, err = part.Write(data)
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, url+"/api/images", &body)
	require.NoError(t, err)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

func makeAnimatedGIF(t *testing.T) []byte {
	t.Helper()

	frame := image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{color.Black, color.White})

	g := &gif.GIF{
		Image: []*image.Paletted{frame, frame},
		Delay: []int{10, 10},
	}

	var buf bytes.Buffer
	require.NoError(t, gif.EncodeAll(&buf, g))

	return buf.Bytes()
}

func TestImageUploadAndGet_PNG(t *testing.T) {
	url, _ := imageTestServer(t)

	resp := postMultipart(t, url, "file", "tiny.png", "image/png", makePNG(t, 8, 8))
	defer closeBody(t, resp.Body)

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out imageUploadResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))

	assert.Len(t, out.ID, 16)
	assert.Equal(t, "/api/images/"+out.ID, out.URL)

	// GET round-trip.
	getResp, err := http.Get(url + out.URL)
	require.NoError(t, err)

	defer closeBody(t, getResp.Body)

	require.Equal(t, http.StatusOK, getResp.StatusCode)
	assert.Equal(t, "image/png", getResp.Header.Get("Content-Type"))
	assert.Equal(t, "public, max-age=31536000, immutable", getResp.Header.Get("Cache-Control"))

	body, err := io.ReadAll(getResp.Body)
	require.NoError(t, err)
	assert.NotEmpty(t, body)
}

func TestImageUpload_Oversize(t *testing.T) {
	url, _ := imageTestServer(t)

	// 12 MB body - exceeds imageUploadEnvelopeBytes; bodyLimit middleware
	// should reject with 413 before reaching the handler.
	big := bytes.Repeat([]byte("x"), 12*1024*1024)

	resp := postMultipart(t, url, "file", "big.bin", "application/octet-stream", big)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusRequestEntityTooLarge, resp.StatusCode)
}

func TestImageUpload_AnimatedGIF(t *testing.T) {
	url, _ := imageTestServer(t)

	resp := postMultipart(t, url, "file", "anim.gif", "image/gif", makeAnimatedGIF(t))
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)

	var ae APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ae))
	assert.Equal(t, ErrCodeImageAnimated, ae.Code)
}

func TestImageUpload_UnsupportedFormat(t *testing.T) {
	url, _ := imageTestServer(t)

	// MP4 magic (ftyp box).
	mp4 := []byte{0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 2, 0}

	resp := postMultipart(t, url, "file", "movie.mp4", "video/mp4", mp4)
	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)

	var ae APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ae))
	assert.Equal(t, ErrCodeImageUnsupported, ae.Code)
}

func TestImageUpload_MissingFileField(t *testing.T) {
	url, _ := imageTestServer(t)

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("not_file", "value"))
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, url+"/api/images", &body)
	require.NoError(t, err)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestImageGet_NotFound(t *testing.T) {
	url, _ := imageTestServer(t)

	resp, err := http.Get(url + "/api/images/" + strings.Repeat("a", 16))
	require.NoError(t, err)

	defer closeBody(t, resp.Body)

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	var ae APIError
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ae))
	assert.Equal(t, ErrCodeImageNotFound, ae.Code)
}

// routingStore wraps a Store and records which project each PutIn names,
// so the handler test can prove the field reaches the store.
type routingStore struct {
	images.Store

	mu       sync.Mutex
	projects []string
}

func (r *routingStore) PutIn(ctx context.Context, project string, raw []byte) (string, string, error) {
	r.mu.Lock()
	r.projects = append(r.projects, project)
	r.mu.Unlock()

	return r.Put(ctx, raw)
}

func (r *routingStore) routed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.projects...)
}

// postImageForProject uploads a PNG under field "file" with an extra
// "project" form field when project is non-empty.
func postImageForProject(t *testing.T, url, project string, data []byte) *http.Response {
	t.Helper()

	var body bytes.Buffer

	writer := multipart.NewWriter(&body)

	if project != "" {
		require.NoError(t, writer.WriteField("project", project))
	}

	part, err := writer.CreateFormFile("file", "shot.png")
	require.NoError(t, err)

	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req, err := http.NewRequest(http.MethodPost, url+"/api/images", &body)
	require.NoError(t, err)

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Requested-With", "contextmatrix")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	return resp
}

func TestImageUpload_ProjectFieldRoutesThroughPutIn(t *testing.T) {
	inner, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = inner.Close() })

	store := &routingStore{Store: inner}
	srv := httptest.NewServer(NewRouter(RouterConfig{ImageStore: store}))
	t.Cleanup(srv.Close)

	resp := postImageForProject(t, srv.URL, "alpha", makePNG(t, 8, 8))
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out imageUploadResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Equal(t, "/api/images/"+out.ID, out.URL)

	resp = postImageForProject(t, srv.URL, "", makePNG(t, 8, 8))
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	assert.Equal(t, []string{"alpha"}, store.routed(), "only the upload that named a project went through PutIn")
}

func TestImageUpload_ProjectFieldIsIgnoredByAPlainStore(t *testing.T) {
	url, store := imageTestServer(t)

	resp := postImageForProject(t, url, "alpha", makePNG(t, 8, 8))
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var out imageUploadResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))

	_, _, err := store.Get(context.Background(), out.ID)
	require.NoError(t, err, "a store without PutIn keeps the blob in images.db")
}

func TestImageUpload_ProjectFieldKeepsTheErrorMapping(t *testing.T) {
	inner, err := images.Open(filepath.Join(t.TempDir(), "images.db"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = inner.Close() })

	srv := httptest.NewServer(NewRouter(RouterConfig{ImageStore: &routingStore{Store: inner}}))
	t.Cleanup(srv.Close)

	resp := postImageForProject(t, srv.URL, "alpha", []byte("not an image"))
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}
