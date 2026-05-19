package images

import (
	"context"
	"errors"
)

// Sentinel errors returned by Store and Process.
var (
	ErrNotFound          = errors.New("images: not found")
	ErrTooLarge          = errors.New("images: payload exceeds 10 MB limit")
	ErrUnsupportedFormat = errors.New("images: unsupported image format")
	ErrAnimated          = errors.New("images: animated GIFs are not supported")
)

// Store persists processed images keyed by content hash. Implementations must
// be safe for concurrent use.
type Store interface {
	// Put processes raw bytes, derives a content-hash ID, and persists the
	// result. Identical payloads are deduplicated — a second Put with the
	// same content returns the existing ID without inserting a new row.
	Put(ctx context.Context, raw []byte) (id string, contentType string, err error)

	// Get retrieves image bytes and content type by ID. Returns ErrNotFound
	// when no row matches.
	Get(ctx context.Context, id string) (data []byte, contentType string, err error)

	// Has reports whether an image with the given ID exists.
	Has(ctx context.Context, id string) (bool, error)

	Close() error
}
