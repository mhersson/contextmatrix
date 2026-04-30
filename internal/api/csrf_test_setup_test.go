package api

import "net/http"

// csrfTestTransport wraps an http.RoundTripper so test requests automatically
// include the X-Requested-With header that csrfGuard now requires on every
// mutation. Without it the ~150 test sites that POST/PUT/PATCH/DELETE via
// http.DefaultClient would all 403 — none of those tests are about the CSRF
// guard itself, so they shouldn't have to know it exists.
//
// Tests that explicitly exercise the CSRF guard (positive and negative cases)
// build their own *http.Client without this transport.
type csrfTestTransport struct{ base http.RoundTripper }

func (t *csrfTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("X-Requested-With") == "" {
		req.Header.Set("X-Requested-With", "contextmatrix")
	}

	return t.base.RoundTrip(req)
}

// init replaces http.DefaultClient with one that forwards through the CSRF
// transport. Called automatically by the test runtime before any test in the
// api package executes.
func init() {
	base := http.DefaultTransport
	http.DefaultClient = &http.Client{Transport: &csrfTestTransport{base: base}}
}
