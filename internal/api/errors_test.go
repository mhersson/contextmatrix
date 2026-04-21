package api

import (
	encodingjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeErrorDetails(t *testing.T) {
	t.Run("nil error returns empty string", func(t *testing.T) {
		assert.Empty(t, sanitizeErrorDetails(nil))
	})

	t.Run("json.SyntaxError returns offset-scoped message", func(t *testing.T) {
		var out map[string]any

		err := encodingjson.Unmarshal([]byte(`{not valid json`), &out)
		details := sanitizeErrorDetails(err)
		assert.Regexp(t, `^invalid JSON at offset \d+$`, details)
	})

	t.Run("json.SyntaxError wrapped with fmt.Errorf still matches via errors.As",
		func(t *testing.T) {
			var out map[string]any

			raw := encodingjson.Unmarshal([]byte(`{`), &out)
			wrapped := fmt.Errorf("decode body: %w", raw)
			details := sanitizeErrorDetails(wrapped)
			assert.Regexp(t, `^invalid JSON at offset \d+$`, details)
		})

	t.Run("json.UnmarshalTypeError returns field-scoped message", func(t *testing.T) {
		type payload struct {
			Name string `json:"name"`
		}

		var p payload

		err := encodingjson.Unmarshal([]byte(`{"name": 123}`), &p)
		details := sanitizeErrorDetails(err)
		assert.Contains(t, details, `invalid type for field "name"`)
		assert.Contains(t, details, "expected string")
	})

	t.Run("json.UnmarshalTypeError without field still returns type info", func(t *testing.T) {
		// A top-level type mismatch — no struct field.
		var target string

		err := encodingjson.Unmarshal([]byte(`42`), &target)
		// 42 into *string is a top-level mismatch; Field is "".
		details := sanitizeErrorDetails(err)
		assert.Contains(t, details, "invalid type")
		assert.Contains(t, details, "expected string")
	})

	t.Run("io.EOF is treated as empty body", func(t *testing.T) {
		assert.Equal(t, "request body is empty", sanitizeErrorDetails(io.EOF))
	})

	t.Run("io.ErrUnexpectedEOF is treated as truncated body", func(t *testing.T) {
		assert.Equal(t, "request body ended unexpectedly",
			sanitizeErrorDetails(io.ErrUnexpectedEOF))
	})

	t.Run("generic json: prefixed error is scrubbed", func(t *testing.T) {
		// Simulate a decoder error that starts with "json:" but is not a
		// typed SyntaxError or UnmarshalTypeError (e.g. json.Decoder with
		// DisallowUnknownFields hitting an unknown field is reported as a
		// plain errors.New value prefixed by "json: ").
		raw := errors.New("json: unknown field \"foo\"")
		details := sanitizeErrorDetails(raw)
		assert.Equal(t, "invalid JSON body", details)
	})

	t.Run("non-JSON error falls through unchanged", func(t *testing.T) {
		raw := errors.New("something else failed")
		assert.Equal(t, "something else failed", sanitizeErrorDetails(raw))
	})

	t.Run("sanitized output never contains filesystem-looking substrings",
		func(t *testing.T) {
			// Defense-in-depth: regardless of error shape, the helper must
			// never echo back strings that resemble filesystem paths from
			// the server environment.
			var out map[string]any

			err := encodingjson.Unmarshal([]byte(`{bad`), &out)
			details := sanitizeErrorDetails(err)
			assert.NotContains(t, details, "/",
				"sanitized details must not contain path separators: %q", details)
		})
}
