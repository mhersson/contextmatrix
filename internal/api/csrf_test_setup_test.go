package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// apiTestTransport wraps an http.RoundTripper so test requests automatically
// include the X-Requested-With header that csrfGuard requires on every
// mutation, and inject X-Agent-ID from the JSON body's `agent_id` field when
// the header is missing. Without these, the ~150 test sites that POST/PUT/
// PATCH/DELETE via http.DefaultClient would all 403 (CSRF) or 400 (missing
// agent_id) — none of those tests are about the CSRF guard or the agent-ID
// header itself, so they shouldn't have to know either exists.
//
// Tests that explicitly exercise the CSRF guard or the X-Agent-ID contract
// (positive and negative cases) build their own *http.Client without this
// transport, so the production header-only enforcement is still verifiable.
type apiTestTransport struct{ base http.RoundTripper }

func (t *apiTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("X-Requested-With") == "" {
		req.Header.Set("X-Requested-With", "contextmatrix")
	}

	if req.Header.Get("X-Agent-ID") == "" && req.Body != nil && req.Body != http.NoBody {
		body, err := io.ReadAll(req.Body)
		if err == nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(body)), nil
			}

			var probe map[string]any
			if json.Unmarshal(body, &probe) == nil {
				if id, ok := probe["agent_id"].(string); ok && id != "" {
					req.Header.Set("X-Agent-ID", id)
				}
			}
		}
	}

	return t.base.RoundTrip(req)
}

// init replaces http.DefaultClient with one that forwards through the test
// transport. Called automatically by the test runtime before any test in the
// api package executes.
func init() {
	base := http.DefaultTransport
	http.DefaultClient = &http.Client{Transport: &apiTestTransport{base: base}}
}
