package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/auth"
	"github.com/mhersson/contextmatrix/internal/authstore"
)

// sessionCookieName is the browser session cookie. The value is the RAW
// 256-bit token; only its SHA-256 lives in auth.db.
const sessionCookieName = "cm_session"

type identityCtxKey struct{}

type sessionUserCtxKey struct{}

// withSessionIdentity stamps the request context with the session's derived
// identity ("human:<username>") and user record.
func withSessionIdentity(ctx context.Context, u *authstore.User) context.Context {
	ctx = context.WithValue(ctx, identityCtxKey{}, "human:"+u.Username)

	return context.WithValue(ctx, sessionUserCtxKey{}, u)
}

// identityFromContext returns the session-derived identity, or "" when the
// request is unauthenticated (none mode, machine channels, exempt paths).
func identityFromContext(ctx context.Context) string {
	v, _ := ctx.Value(identityCtxKey{}).(string)

	return v
}

// sessionIdentity reports whether the request carries a valid authenticated
// session. ok is true exactly when the request arrived with a session cookie
// that resolved to a real user (multi mode); false means none mode or a
// machine channel.
func sessionIdentity(ctx context.Context) (id string, ok bool) {
	id = identityFromContext(ctx)

	return id, id != ""
}

// sessionUserFromContext returns the logged-in user, or nil.
func sessionUserFromContext(ctx context.Context) *authstore.User {
	u, _ := ctx.Value(sessionUserCtxKey{}).(*authstore.User)

	return u
}

// requestIsTLS reports whether the request arrived over TLS, directly or via
// a TLS-terminating proxy. Drives the cookie Secure flag with no config knob
// so plain-HTTP LAN deployments still work.
func requestIsTLS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// setSessionCookie writes the session cookie: HttpOnly + SameSite=Lax always,
// Secure when the deployment speaks TLS.
func setSessionCookie(w http.ResponseWriter, r *http.Request, raw string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsTLS(r),
	})
}

// clearSessionCookie expires the session cookie.
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsTLS(r),
	})
}

// sessionGuard resolves the session cookie on EVERY request (so exempt
// handlers like /api/app/config can still see who is asking) and enforces
// authentication on non-exempt paths. Renewals refresh the cookie max-age.
func sessionGuard(svc *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var user *authstore.User

			if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
				if res, err := svc.ValidateSession(r.Context(), c.Value); err == nil {
					user = res.User

					if res.Renewed {
						setSessionCookie(w, r, c.Value, svc.IdleTTL())
					}

					r = r.WithContext(withSessionIdentity(r.Context(), user))
				} else {
					// The browser presented a cookie that no longer maps to a
					// live session — expire it so subsequent requests stop
					// re-sending (and re-validating) the dead value.
					clearSessionCookie(w, r)
				}
			}

			if user == nil && !sessionExempt(r) {
				writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", "")

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// sessionExempt lists the paths reachable without a session in multi mode.
// Browser-facing routes under a machine prefix are carved back OUT of the
// exemption — any new browser route must be gated, not exempted.
func sessionExempt(r *http.Request) bool {
	path := r.URL.Path

	// Browser-facing despite the /api/runner/ prefix: the web UI's log
	// stream and capacity meter.
	if path == "/api/runner/logs" || path == "/api/runner/health" {
		return false
	}

	switch {
	case path == "/healthz" || path == "/readyz" || path == "/mcp":
		// Probes and the Bearer-authed MCP endpoint.
		return true
	case strings.HasPrefix(path, "/api/auth/"):
		// Login and token redemption must work logged-out; authenticated
		// auth endpoints (session, password) check the context themselves.
		return true
	case path == "/api/app/config":
		// Pre-login the SPA needs theme + auth_mode; the handler serves a
		// slim shape when unauthenticated.
		return true
	case strings.HasPrefix(path, "/api/runner/"),
		strings.HasPrefix(path, "/api/agent/"),
		strings.HasPrefix(path, "/api/chat/"),
		strings.HasPrefix(path, "/api/v1/"):
		// HMAC-signed backend-callback space (and the backend-called
		// autonomous check) — machine channels with their own auth.
		return true
	case strings.HasPrefix(path, "/api/worker/"):
		// Bearer-authed worker-callback space (GET /api/worker/git-credentials)
		// — its own per-session auth, independent of the session cookie.
		// Mirrors /mcp's treatment: a machine channel with its own auth, not a
		// browser route.
		return true
	}

	return false
}

// ErrCodeTokenInvalid covers one-time-token failures: 404 for unknown
// tokens, 410 Gone for spent/expired ones (details disambiguate).
const ErrCodeTokenInvalid = "TOKEN_INVALID"

// authHandlers maps auth.Service onto /api/auth/*.
type authHandlers struct {
	svc *auth.Service
}

type sessionResponse struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

func toSessionResponse(u *authstore.User) sessionResponse {
	return sessionResponse{Username: u.Username, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin}
}

// login handles POST /api/auth/login.
func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	user, raw, err := h.svc.Login(r.Context(), req.Username, req.Password, auth.ClientIP(r.RemoteAddr))
	if err != nil {
		var rle *auth.RateLimitedError

		if errors.As(err, &rle) {
			w.Header().Set("Retry-After", strconv.Itoa(int(rle.RetryAfter.Seconds())+1))
			writeError(w, http.StatusTooManyRequests, ErrCodeRateLimited, "too many attempts", "")

			return
		}

		// Uniform 401 — never reveals whether the username exists.
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "invalid credentials", "")

		return
	}

	setSessionCookie(w, r, raw, h.svc.IdleTTL())
	writeJSON(w, http.StatusOK, toSessionResponse(user))
}

// logout handles POST /api/auth/logout. Idempotent 204.
func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = h.svc.Logout(r.Context(), c.Value)
	}

	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// getSession handles GET /api/auth/session — "who am I". The path is
// session-exempt (login page probes it), so the handler enforces auth itself.
func (h *authHandlers) getSession(w http.ResponseWriter, r *http.Request) {
	user := sessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", "")

		return
	}

	writeJSON(w, http.StatusOK, toSessionResponse(user))
}

// inspectToken handles GET /api/auth/token/{token}.
func (h *authHandlers) inspectToken(w http.ResponseWriter, r *http.Request) {
	info, err := h.svc.InspectToken(r.Context(), r.PathValue("token"))
	if err != nil {
		writeTokenError(w, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"purpose":  string(info.Purpose),
		"username": info.Username,
	})
}

// redeemToken handles POST /api/auth/token/{token} — burns the token, sets
// the password (creating the admin account for bootstrap), and auto-logs-in.
func (h *authHandlers) redeemToken(w http.ResponseWriter, r *http.Request) {
	rawToken := r.PathValue("token")

	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	info, err := h.svc.InspectToken(r.Context(), rawToken)
	if err != nil {
		writeTokenError(w, err)

		return
	}

	var (
		user       *authstore.User
		sessionRaw string
	)

	if info.Purpose == authstore.TokenPurposeBootstrap {
		user, sessionRaw, err = h.svc.RedeemBootstrap(r.Context(), rawToken, req.Username, req.DisplayName, req.Password)
	} else {
		user, sessionRaw, err = h.svc.RedeemInviteOrReset(r.Context(), rawToken, req.Password)
	}

	if err != nil {
		switch {
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, err.Error(), "")
		case errors.Is(err, authstore.ErrInvalidUsername):
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "invalid username", "1-32 chars: a-z 0-9 . _ -, no leading/trailing punctuation")
		case errors.Is(err, authstore.ErrDuplicate):
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, "username already taken", "")
		case errors.Is(err, auth.ErrNotBootstrappable):
			writeError(w, http.StatusConflict, ErrCodeValidationError, "users already exist — bootstrap is closed", "")
		default:
			writeTokenError(w, err)
		}

		return
	}

	setSessionCookie(w, r, sessionRaw, h.svc.IdleTTL())
	writeJSON(w, http.StatusOK, toSessionResponse(user))
}

// changePassword handles POST /api/auth/password. Session-exempt path, so
// it enforces auth itself.
func (h *authHandlers) changePassword(w http.ResponseWriter, r *http.Request) {
	user := sessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", "")

		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, ErrCodeBadRequest, "invalid JSON body", "")

		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "authentication required", "")

		return
	}

	if err := h.svc.ChangePassword(r.Context(), user.ID, req.CurrentPassword, req.NewPassword, cookie.Value); err != nil {
		switch {
		case errors.Is(err, auth.ErrPasswordTooShort):
			writeError(w, http.StatusUnprocessableEntity, ErrCodeValidationError, err.Error(), "")
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, ErrCodeUnauthorized, "current password is wrong", "")
		default:
			handleServiceError(w, r, err)
		}

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeTokenError maps token sentinels: unknown → 404, spent/expired → 410.
func writeTokenError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrTokenSpent):
		writeError(w, http.StatusGone, ErrCodeTokenInvalid, "link already used", "")
	case errors.Is(err, auth.ErrTokenExpired):
		writeError(w, http.StatusGone, ErrCodeTokenInvalid, "link expired — ask an admin for a new one", "")
	default:
		writeError(w, http.StatusNotFound, ErrCodeTokenInvalid, "unknown link", "")
	}
}
