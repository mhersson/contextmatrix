package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
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

// blueprintSlicePattern is a slice id's shape (SL<n>, n from 1); only a
// well-formed id is forwarded in the architect's query.
var blueprintSlicePattern = regexp.MustCompile(`^SL[1-9][0-9]{0,2}$`)

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

// get signs and sends a GET for path with query. The random nonce keeps two
// same-second requests for one path from signing alike, which the
// architect's replay cache would refuse.
func (u *architectUpstream) get(ctx context.Context, path string, query url.Values, accept string) (*http.Response, error) {
	q := url.Values{"nonce": {rand.Text()}}
	for k, v := range query {
		q[k] = v
	}

	fullURL := u.baseURL + path + "?" + q.Encode()

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
func (h *chatHandlers) architectGet(ctx context.Context, w http.ResponseWriter, r *http.Request, path string, query url.Values, accept string) (*http.Response, bool) {
	logger := ctxlog.Logger(r.Context()).With("upstream_path", path)

	resp, err := h.architect.get(ctx, path, query, accept)
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
// application/json so CM's origin never serves another content type. tee,
// when non-nil, receives a copy of the body. It reports whether the whole
// body was relayed.
func (h *chatHandlers) proxyBlueprintRead(w http.ResponseWriter, r *http.Request, path string, query url.Values, tee io.Writer) bool {
	ctx, cancel := context.WithTimeout(r.Context(), h.architect.readTimeout)
	defer cancel()

	resp, ok := h.architectGet(ctx, w, r, path, query, "application/json")
	if !ok {
		return false
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	var body io.Reader = resp.Body
	if tee != nil {
		body = io.TeeReader(resp.Body, tee)
	}

	if _, err := io.Copy(w, body); err != nil {
		if r.Context().Err() == nil {
			ctxlog.Logger(r.Context()).Warn("blueprint proxy: relay cut short", "upstream_path", path, "error", err)
		}

		return false
	}

	return true
}

// getBlueprint handles GET /api/chats/{id}/blueprint and records the view's
// cost.spent_usd on the session.
func (h *chatHandlers) getBlueprint(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.blueprintSession(w, r)
	if !ok {
		return
	}

	var view bytes.Buffer
	if !h.proxyBlueprintRead(w, r, "/blueprints/"+url.PathEscape(sess.ID), nil, &view) {
		return
	}

	spent, ok := readSpend(view.Bytes())
	if !ok {
		return
	}

	// The response is already written, so a caller leaving now must not cancel the write.
	if err := h.mgr.RecordBlueprintSpend(context.WithoutCancel(r.Context()), sess.ID, spent); err != nil {
		ctxlog.Logger(r.Context()).Warn("blueprint view: record spend failed", "session_id", sess.ID, "error", err)
	}
}

// readSpend reads cost.spent_usd from a blueprint view body; ok is false when
// the body is not JSON or carries no cost or no spend.
func readSpend(body []byte) (float64, bool) {
	var view struct {
		Cost *struct {
			SpentUSD *float64 `json:"spent_usd"`
		} `json:"cost"`
	}

	if err := json.Unmarshal(body, &view); err != nil || view.Cost == nil || view.Cost.SpentUSD == nil {
		return 0, false
	}

	return *view.Cost.SpentUSD, true
}

// ArchitectSpendReader reads a blueprint's cost.spent_usd with one signed
// GET /blueprints/{id}, bounded by architectReadTimeout. A non-200 answer is
// an error; ok is false when the view carries no spend.
func ArchitectSpendReader(baseURL, key string) func(ctx context.Context, sessionID string) (float64, bool, error) {
	upstream := newArchitectUpstream(baseURL, key)

	return func(ctx context.Context, sessionID string) (float64, bool, error) {
		ctx, cancel := context.WithTimeout(ctx, upstream.readTimeout)
		defer cancel()

		path := "/blueprints/" + url.PathEscape(sessionID)

		resp, err := upstream.get(ctx, path, nil, "application/json")
		if err != nil {
			return 0, false, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return 0, false, fmt.Errorf("architect GET %s: status %d", path, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, false, fmt.Errorf("architect GET %s: read body: %w", path, err)
		}

		spent, ok := readSpend(body)

		return spent, ok, nil
	}
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

	query, sliceOK := blueprintSliceQuery(w, r)
	if !sliceOK {
		return
	}

	sess, ok := h.blueprintSession(w, r)
	if !ok {
		return
	}

	h.proxyBlueprintRead(w, r, "/blueprints/"+url.PathEscape(sess.ID)+"/artifacts/"+stage+"/"+strconv.Itoa(version), query, nil)
}

// blueprintSliceQuery reads the optional slice parameter: absent forwards
// nothing, one well-formed id forwards it, anything else (including an
// undecodable query) answers 400. ok=false means a response was written.
func blueprintSliceQuery(w http.ResponseWriter, r *http.Request) (url.Values, bool) {
	// r.URL.Query() silently drops pairs it cannot decode, which would read as absent.
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid query", "")

		return nil, false
	}

	vals, present := q["slice"]
	if !present {
		return nil, true
	}

	if len(vals) != 1 || !blueprintSlicePattern.MatchString(vals[0]) {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid slice", strings.Join(vals, ","))

		return nil, false
	}

	return url.Values{"slice": vals}, true
}
