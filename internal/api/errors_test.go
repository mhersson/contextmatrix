package api

import (
	encodingjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSanitizeErrorDetails_Strings verifies that downstream error strings with
// leakable content are replaced by stable class labels, while author-written
// short messages pass through unchanged.
func TestSanitizeErrorDetails_Strings(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "go-git transport auth failure collapses to class label",
			in:   "clone: transport: authentication required",
			want: "git remote unreachable",
		},
		{
			name: "go-git transport prefix at start collapses to class label",
			in:   "transport.ErrEmptyRemoteRepository",
			want: "git remote unreachable",
		},
		{
			name: "ssh handshake failure collapses to class label",
			in:   "push: ssh: handshake failed: host key mismatch",
			want: "git remote unreachable",
		},
		{
			name: "exec missing binary collapses to class label",
			in:   "run git: exec: \"git\": executable file not found in $PATH",
			want: "git operation failed",
		},
		{
			name: "boards-dir .git path leak is redacted",
			in:   "open /var/lib/contextmatrix/boards/.git/refs/heads/main: no such file or directory",
			want: "git operation failed",
		},
		{
			name: "generic absolute path leak is redacted",
			in:   "open /home/alice/boards/project/tasks/CARD-001.md: permission denied",
			want: "filesystem error",
		},
		{
			name: "short author-written message passes through unchanged",
			in:   "card already claimed by agent-7",
			want: "card already claimed by agent-7",
		},
		{
			name: "validation-style message passes through unchanged",
			in:   `parent card "TEST-042" does not exist`,
			want: `parent card "TEST-042" does not exist`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeErrorDetails(errors.New(tc.in))
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestSanitizeErrorDetails_JSON covers the typed-error and stdlib-decoder
// branches of the sanitizer so JSON parse failures never echo unparsed body
// fragments back to the client.
func TestSanitizeErrorDetails_JSON(t *testing.T) {
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
		var target string

		err := encodingjson.Unmarshal([]byte(`42`), &target)
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
			var out map[string]any

			err := encodingjson.Unmarshal([]byte(`{bad`), &out)
			details := sanitizeErrorDetails(err)
			assert.NotContains(t, details, "/",
				"sanitized details must not contain path separators: %q", details)
		})
}

// TestErrProjectNotFound_SentinelIdentity pins the invariant that
// storage.ErrProjectNotFound and board.ErrProjectNotFound resolve to the same
// underlying sentinel. Without this identity, errors originating in the board
// package (e.g. board.LoadProjectConfig) silently bypass the 404 branch of
// handleServiceError that only checks storage.ErrProjectNotFound.
func TestErrProjectNotFound_SentinelIdentity(t *testing.T) {
	assert.Same(t, board.ErrProjectNotFound, storage.ErrProjectNotFound,
		"storage.ErrProjectNotFound must alias board.ErrProjectNotFound")
	require.ErrorIs(t, board.ErrProjectNotFound, storage.ErrProjectNotFound)
	require.ErrorIs(t, storage.ErrProjectNotFound, board.ErrProjectNotFound)
}

// TestHandleServiceError_BoardProjectNotFound_Returns404 exercises the
// handleServiceError dispatch path with an error chain rooted in
// board.ErrProjectNotFound (as produced by board.LoadProjectConfig) and
// verifies it is routed to 404 PROJECT_NOT_FOUND rather than falling through
// to the generic 500 INTERNAL_ERROR branch.
func TestHandleServiceError_BoardProjectNotFound_Returns404(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "raw board sentinel",
			err:  board.ErrProjectNotFound,
		},
		{
			name: "board sentinel wrapped by service layer",
			err:  fmt.Errorf("load project config: %w", board.ErrProjectNotFound),
		},
		{
			name: "raw storage sentinel (aliased; same underlying value)",
			err:  storage.ErrProjectNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/projects/ghost", nil)

			handleServiceError(rec, req, tc.err)

			assert.Equal(t, http.StatusNotFound, rec.Code)

			var apiErr APIError
			require.NoError(t, encodingjson.NewDecoder(rec.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeProjectNotFound, apiErr.Code)
		})
	}
}

// TestHandleServiceError_PromoteRequiresHuman verifies that
// service.ErrPromoteRequiresHuman is mapped to 403 HUMAN_ONLY_FIELD instead
// of falling through to the generic 500 branch.
func TestHandleServiceError_PromoteRequiresHuman(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/projects/p/cards/X-1/promote", nil)

	wrapped := fmt.Errorf("promote card X-1: %w", service.ErrPromoteRequiresHuman)
	handleServiceError(rec, req, wrapped)

	assert.Equal(t, http.StatusForbidden, rec.Code)

	var apiErr APIError
	require.NoError(t, encodingjson.NewDecoder(rec.Body).Decode(&apiErr))
	assert.Equal(t, ErrCodeHumanOnlyField, apiErr.Code)
}

// TestExtractAgentID_HeaderOnly is a regression guard: extractAgentID must
// return only the header value. The previous body-field fallback (deleted to
// preserve the human:-prefix gate) would have allowed a request with an
// empty header but agent_id="human:alice" in body to claim as Alice.
func TestExtractAgentID_HeaderOnly(t *testing.T) {
	t.Run("header populated wins", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/projects/p/cards/X-1/claim", nil)
		req.Header.Set("X-Agent-ID", "human:alice")

		assert.Equal(t, "human:alice", extractAgentID(req))
	})

	t.Run("header missing returns empty even when body would have agent_id", func(t *testing.T) {
		// Body content is irrelevant; extractAgentID never reads it.
		req := httptest.NewRequest(http.MethodPost, "/api/projects/p/cards/X-1/claim", nil)
		assert.Empty(t, extractAgentID(req))
	})

	t.Run("whitespace-only header returns empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/projects/p/cards/X-1/claim", nil)
		req.Header.Set("X-Agent-ID", "  ")

		assert.Empty(t, extractAgentID(req))
	})
}

// TestHandleServiceError_BoardValidationSentinels verifies that the
// validation-error sentinels added to the 422 branch (ErrInvalidExternalURL,
// ErrInvalidRunnerStatus) are mapped to 422 VALIDATION_ERROR instead of
// falling through to the generic 500 branch.
func TestHandleServiceError_BoardValidationSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "ErrInvalidExternalURL wrapped in ValidationError",
			err: &board.ValidationError{
				Err:     board.ErrInvalidExternalURL,
				Field:   "source.external_url",
				Value:   "javascript:alert(1)",
				Message: "scheme must be http or https",
			},
		},
		{
			name: "ErrInvalidRunnerStatus wrapped in ValidationError",
			err: &board.ValidationError{
				Err:     board.ErrInvalidRunnerStatus,
				Field:   "runner_status",
				Value:   "bogus",
				Message: "invalid status",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/projects/p/cards/X-1", nil)

			handleServiceError(rec, req, tc.err)

			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

			var apiErr APIError
			require.NoError(t, encodingjson.NewDecoder(rec.Body).Decode(&apiErr))
			assert.Equal(t, ErrCodeValidationError, apiErr.Code)
		})
	}
}
