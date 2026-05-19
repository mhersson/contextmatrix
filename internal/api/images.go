package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
	"github.com/mhersson/contextmatrix/internal/images"
)

// imageUploadMaxBytes caps the multipart request body for /api/images.
// 10 MB for the image plus 1 MB headroom for the multipart envelope.
const imageUploadMaxBytes = 11 * 1024 * 1024

// Error codes specific to image upload.
const (
	ErrCodeImageNotFound       = "IMAGE_NOT_FOUND"
	ErrCodeImageUnsupported    = "IMAGE_UNSUPPORTED"
	ErrCodeImageAnimated       = "IMAGE_ANIMATED"
	ErrCodeImageMissingFile    = "IMAGE_MISSING_FILE"
	ErrCodeImageInvalidPayload = "IMAGE_INVALID_PAYLOAD"
)

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

// upload accepts a multipart form with a `file` field, stores it via images.Store,
// and returns the content-hash id plus public URL.
func (h *imageHandlers) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(imageUploadMaxBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
				"upload exceeds 10 MB limit", "")

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
	raw, err := io.ReadAll(io.LimitReader(file, imageUploadMaxBytes+1))
	if err != nil {
		ctxlog.Logger(r.Context()).Warn("image upload read failed", "error", err)
		writeError(w, http.StatusBadRequest, ErrCodeImageInvalidPayload,
			"failed to read upload", "")

		return
	}

	id, _, err := h.store.Put(r.Context(), raw)
	if err != nil {
		switch {
		case errors.Is(err, images.ErrTooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, ErrCodeContentTooLarge,
				"image exceeds 10 MB limit", "")
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

	if _, err := w.Write(data); err != nil {
		ctxlog.Logger(r.Context()).Warn("write image response failed", "id", id, "error", err)
	}
}
