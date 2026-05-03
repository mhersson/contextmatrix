package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// csrfTestTransport wraps an http.RoundTripper so test requests automatically
// include the X-Requested-With header that csrfGuard now requires on every
// mutation, and the X-Agent-ID header derived from any JSON body that carries
// an agent_id field. Existing tests POST agent_id in the body and never set
// the header — after the body-fallback for agent identity was removed they
// would all 400 unless the test transport copies the field up. Tests that
// explicitly exercise the CSRF guard or the missing-header behavior build
// their own *http.Client without this transport.
type csrfTestTransport struct{ base http.RoundTripper }

func (t *csrfTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("X-Requested-With") == "" {
		req.Header.Set("X-Requested-With", "contextmatrix")
	}

	if req.Header.Get("X-Agent-ID") == "" && req.Body != nil {
		buf, err := io.ReadAll(req.Body)
		_ = req.Body.Close()

		if err == nil && len(buf) > 0 {
			var probe struct {
				AgentID string `json:"agent_id"`
			}
			if json.Unmarshal(buf, &probe) == nil && probe.AgentID != "" {
				req.Header.Set("X-Agent-ID", probe.AgentID)
			}

			req.Body = io.NopCloser(bytes.NewReader(buf))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(buf)), nil
			}
		}
	}

	return t.base.RoundTrip(req)
}

// init replaces http.DefaultClient with one that forwards through the test
// transport. Called automatically by the test runtime before any test in
// the api package executes.
func init() {
	base := http.DefaultTransport
	http.DefaultClient = &http.Client{Transport: &csrfTestTransport{base: base}}
}
