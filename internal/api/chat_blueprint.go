package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/chat"
	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// ErrCodeBlueprintNotFound → 404: the session is not a blueprint, or the
// architect does not serve that blueprint, artifact or route.
const ErrCodeBlueprintNotFound = "BLUEPRINT_NOT_FOUND"

// architectReadTimeout bounds one proxied view or artifact read.
const architectReadTimeout = 30 * time.Second

// blueprintStagePattern keeps the stage segment to a stage id's shape, so a
// decoded slash or dot segment never reaches the architect's URL.
var blueprintStagePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// architectUpstream performs the signed GETs behind the blueprint proxies.
type architectUpstream struct {
	baseURL string
	key     string
	// httpc has no timeout: the live relay is long-lived, and reads take
	// their deadline from readTimeout.
	httpc       *http.Client
	readTimeout time.Duration
}

func newArchitectUpstream(baseURL, key string) *architectUpstream {
	return &architectUpstream{
		baseURL:     strings.TrimRight(baseURL, "/"),
		key:         key,
		httpc:       &http.Client{},
		readTimeout: architectReadTimeout,
	}
}

// get signs and sends a GET for path. The random nonce keeps two same-second
// requests for one path from signing alike, which the architect's replay
// cache would refuse.
func (u *architectUpstream) get(ctx context.Context, path, accept string) (*http.Response, error) {
	fullURL := u.baseURL + path + "?nonce=" + rand.Text()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("architect GET %s: build request: %w", path, err)
	}

	sig, ts := protocol.SignRequestHeaders(u.key, http.MethodGet, req.URL.RequestURI(), nil)

	req.Header.Set("Accept", accept)
	req.Header.Set(protocol.SignatureHeader, sig)
	req.Header.Set(protocol.TimestampHeader, ts)

	resp, err := u.httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("architect GET %s: %w", path, err)
	}

	return resp, nil
}

// blueprintSession resolves the caller's architect session for a blueprint
// proxy. Ownership is checked before the kind, so a foreign session answers
// exactly like a missing one. ok=false means a response was written.
func (h *chatHandlers) blueprintSession(w http.ResponseWriter, r *http.Request) (chat.Session, bool) {
	sess, ok := h.ownedSession(w, r)
	if !ok {
		return chat.Session{}, false
	}

	if sess.Kind != chat.KindArchitect {
		writeError(w, http.StatusNotFound, ErrCodeBlueprintNotFound, "chat session has no blueprint", "")

		return chat.Session{}, false
	}

	if h.architect == nil {
		writeError(w, http.StatusConflict, ErrCodeChatKindUnavailable, "no backend configured for this session kind", "")

		return chat.Session{}, false
	}

	return sess, true
}

// architectGet sends the upstream GET and returns the response only on a
// 200. Otherwise it answers the caller itself: 404 BLUEPRINT_NOT_FOUND for an
// upstream 404, 502 BACKEND_UNAVAILABLE for any other failure, nothing when
// the caller already left. ok=false means the caller must not write.
func (h *chatHandlers) architectGet(ctx context.Context, w http.ResponseWriter, r *http.Request, path, accept string) (*http.Response, bool) {
	logger := ctxlog.Logger(r.Context()).With("upstream_path", path)

	resp, err := h.architect.get(ctx, path, accept)
	if err != nil {
		if r.Context().Err() != nil {
			return nil, false
		}

		logger.Warn("blueprint proxy: architect unreachable", "error", err)
		writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "architect is unreachable", "")

		return nil, false
	}

	if resp.StatusCode == http.StatusOK {
		return resp, true
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		writeError(w, http.StatusNotFound, ErrCodeBlueprintNotFound, "blueprint not found", "")

		return nil, false
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 512))
	logger.Warn("blueprint proxy: architect refused the request",
		"status", resp.StatusCode, "body", string(body), "read_error", readErr)
	writeError(w, http.StatusBadGateway, ErrCodeBackendUnavailable, "architect refused the request", "")

	return nil, false
}

// proxyBlueprintRead relays one architect JSON read byte for byte, always as
// application/json so CM's origin never serves another content type.
func (h *chatHandlers) proxyBlueprintRead(w http.ResponseWriter, r *http.Request, path string) {
	ctx, cancel := context.WithTimeout(r.Context(), h.architect.readTimeout)
	defer cancel()

	resp, ok := h.architectGet(ctx, w, r, path, "application/json")
	if !ok {
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, resp.Body); err != nil && r.Context().Err() == nil {
		ctxlog.Logger(r.Context()).Warn("blueprint proxy: relay cut short", "upstream_path", path, "error", err)
	}
}

// getBlueprint handles GET /api/chats/{id}/blueprint.
func (h *chatHandlers) getBlueprint(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.blueprintSession(w, r)
	if !ok {
		return
	}

	h.proxyBlueprintRead(w, r, "/blueprints/"+url.PathEscape(sess.ID))
}

// getBlueprintArtifact handles
// GET /api/chats/{id}/blueprint/artifacts/{stage}/{version}.
func (h *chatHandlers) getBlueprintArtifact(w http.ResponseWriter, r *http.Request) {
	stage := r.PathValue("stage")
	if !blueprintStagePattern.MatchString(stage) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid stage", stage)

		return
	}

	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid version", r.PathValue("version"))

		return
	}

	sess, ok := h.blueprintSession(w, r)
	if !ok {
		return
	}

	h.proxyBlueprintRead(w, r, "/blueprints/"+url.PathEscape(sess.ID)+"/artifacts/"+stage+"/"+strconv.Itoa(version))
}

// streamBlueprint handles GET /api/chats/{id}/blueprint/live. It relays the
// architect's live SSE stream byte for byte and flushes every chunk, so each
// frame reaches the browser as the architect sends it. The upstream request
// rides the browser's context: a browser that leaves closes it, and an
// upstream that ends or fails a read ends the browser's response.
func (h *chatHandlers) streamBlueprint(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.blueprintSession(w, r)
	if !ok {
		return
	}

	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		ctxlog.Logger(r.Context()).Warn("blueprint live: could not clear write deadline; the stream drops on WriteTimeout",
			"error", err)
	}

	path := "/blueprints/" + url.PathEscape(sess.ID) + "/live"

	resp, ok := h.architectGet(r.Context(), w, r, path, "text/event-stream")
	if !ok {
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	if err := rc.Flush(); err != nil {
		return
	}

	buf := make([]byte, 32*1024)

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			// A failed write returns, closing both sides; nothing is ever skipped.
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}

			if ferr := rc.Flush(); ferr != nil {
				return
			}
		}

		if err != nil {
			if !errors.Is(err, io.EOF) && r.Context().Err() == nil {
				ctxlog.Logger(r.Context()).Debug("blueprint live: upstream ended", "session_id", sess.ID, "error", err)
			}

			return
		}
	}
}
