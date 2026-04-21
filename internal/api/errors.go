package api

import (
	encodingjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// sanitizeErrorDetails converts an error into a stable, sanitized detail
// string suitable for inclusion in a client-facing error response body.
//
// JSON decode errors from stdlib can expose low-level parser internals and
// sometimes embed unquoted input characters. Routing them through this
// helper trims that surface to a fixed set of descriptive messages.
//
// For non-JSON errors the full error string is returned unchanged — callers
// that want sanitization there should pre-process the error before calling.
func sanitizeErrorDetails(err error) string {
	if err == nil {
		return ""
	}

	var syntaxErr *encodingjson.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Sprintf("invalid JSON at offset %d", syntaxErr.Offset)
	}

	var typeErr *encodingjson.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			return fmt.Sprintf("invalid type: expected %s", typeErr.Type)
		}

		return fmt.Sprintf("invalid type for field %q: expected %s", field, typeErr.Type)
	}

	if errors.Is(err, io.EOF) {
		return "request body is empty"
	}

	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "request body ended unexpectedly"
	}

	// Catch-all for remaining JSON decoder errors that don't match a typed
	// error. The wrapping text is produced by the stdlib with stable phrasing;
	// match it by prefix and return a scrubbed form instead of the raw string.
	msg := err.Error()
	if strings.HasPrefix(msg, "json: ") {
		return "invalid JSON body"
	}

	return msg
}
