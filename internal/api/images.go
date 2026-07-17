package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/images"
)

// imageUploadEnvelopeBytes caps the *multipart envelope* body for /api/images.
// images.MaxUploadBytes is the size cap on the image payload itself;
// multipartEnvelopeHeadroom covers the boundary, part headers, and field
// trailers. Anything beyond this in the envelope is rejected by bodyLimit
// before the handler runs.
const multipartEnvelopeHeadroom = 1 * 1024 * 1024

const imageUploadEnvelopeBytes = images.MaxUploadBytes + multipartEnvelopeHeadroom

// multipartInMemoryBytes is the in-memory threshold for ParseMultipartForm.
// Anything beyond this spills to a temp file on disk, so the per-request
// heap footprint stays bounded under concurrent uploads. The temp file is
// cleaned up via deferred MultipartForm.RemoveAll() in the handler.
const multipartInMemoryBytes = 1 * 1024 * 1024

// imageIDPattern is the canonical content-hash ID shape. Anything else is
// rejected at handler entry - keeping malformed user-controlled path segments
// out of logs and SQL. The pattern fragment is owned by the images package
// so changing the ID shape is a one-line edit there.
var imageIDPattern = regexp.MustCompile(`^` + images.IDPatternFragment + `$`)

type imageHandlers struct {
	store images.Store
}

func newImageHandlers(store images.Store) *imageHandlers {
	return &imageHandlers{store: store}
}

type imageUploadResponse struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// imageTooLargeMessage builds the user-facing "image exceeds N MB" message
// using the canonical cap from the images package so the copy never drifts
// from the actual byte limit.
func imageTooLargeMessage() string {
	return fmt.Sprintf("image exceeds %d MB limit", images.MaxUploadBytes/(1<<20))
}

// upload accepts a multipart form with a `file` field, stores it via images.Store,
// and returns the content-hash id plus public URL.
func (h *imageHandlers) upload(w http.ResponseWriter, r *http.Request) {
	// Register the temp-file cleanup *before* ParseMultipartForm runs. The
	// stdlib parser allocates temp files lazily as it spills parts; a
	// mid-stream error can leave earlier parts on disk with r.MultipartForm
	// already populated. Hoisting the defer up makes the cleanup
	// unconditional on every exit path.
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	if err := r.ParseMultipartForm(multipartInMemoryBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
				imageTooLargeMessage(), "")

			return
		}

		writeError(w, http.StatusBadRequest, ErrCodeImageInvalidPayload,
			"invalid multipart form", "")

		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeImageMissingFile,
			"multipart form missing `file` field", "")

		return
	}
	defer file.Close()

	// LimitReader guards against a multipart part claiming a smaller size
	// than its actual content (Content-Length headers on parts are advisory).
	// Read one byte over the cap so we can distinguish "exactly the cap" from
	// "over the cap" and surface a precise 413 instead of silently truncating
	// into an ErrUnsupportedFormat from the decoder.
	raw, err := io.ReadAll(io.LimitReader(file, images.MaxUploadBytes+1))
	if err != nil {
		ctxlog.Logger(r.Context()).Warn("image upload read failed", "error", err)
		writeError(w, http.StatusBadRequest, ErrCodeImageInvalidPayload,
			"failed to read upload", "")

		return
	}

	if len(raw) > images.MaxUploadBytes {
		writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
			imageTooLargeMessage(), "")

		return
	}

	id, _, err := h.store.Put(r.Context(), raw)
	if err != nil {
		switch {
		case errors.Is(err, images.ErrTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
				imageTooLargeMessage(), "")
		case errors.Is(err, images.ErrUnsupportedFormat):
			writeError(w, http.StatusUnsupportedMediaType, ErrCodeImageUnsupported,
				"unsupported image format", "supported: png, jpeg, gif, webp")
		case errors.Is(err, images.ErrAnimated):
			writeError(w, http.StatusUnsupportedMediaType, ErrCodeImageAnimated,
				"animated GIFs are not supported", "")
		default:
			ctxlog.Logger(r.Context()).Error("image put failed", "error", err)
			writeError(w, http.StatusInternalServerError, ErrCodeInternalError,
				"failed to store image", "")
		}

		return
	}

	writeJSON(w, http.StatusCreated, imageUploadResponse{
		ID:  id,
		URL: "/api/images/" + id,
	})
}

// get serves stored image bytes with the original content type and a long
// immutable cache header. Content-hash IDs guarantee bytes never change for a
// given URL, so aggressive caching is safe.
func (h *imageHandlers) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	// Reject anything that doesn't match the canonical ID shape before
	// touching the store. The mux pattern accepts any non-slash segment, so
	// an unbounded id would otherwise inflate log lines and hit the SQL
	// path with arbitrary user-controlled bytes.
	if !imageIDPattern.MatchString(id) {
		writeError(w, http.StatusNotFound, ErrCodeImageNotFound, "image not found", "")

		return
	}

	data, contentType, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, images.ErrNotFound) {
			writeError(w, http.StatusNotFound, ErrCodeImageNotFound, "image not found", "")

			return
		}

		ctxlog.Logger(r.Context()).Error("image get failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError,
			"failed to read image", "")

		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))

	// #nosec G705 -- false positive: data is image bytes re-encoded through
	// stdlib decoders/encoders during ingest (internal/images/processor.go),
	// which strips any embedded HTML/script content. Content-Type is set
	// explicitly above; the router sets X-Content-Type-Options: nosniff
	// globally so browsers cannot reinterpret the response as HTML.
	if _, err := w.Write(data); err != nil {
		ctxlog.Logger(r.Context()).Warn("write image response failed", "id", id, "error", err)
	}
}
