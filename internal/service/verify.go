package service

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix/internal/board"
)

// ErrInvalidVerify is returned when a card's verify config fails validation.
// The API layer maps it to 422, matching the project path's
// board.ErrInvalidProjectConfig.
var ErrInvalidVerify = errors.New("invalid verify config")

const (
	// maxVerifyCommandLen caps the verify command length. Hygiene only - a
	// verify command is a single shell line, not a script.
	maxVerifyCommandLen = 1024
	// maxVerifyTimeoutSeconds bounds the verify subprocess timeout (2 hours).
	maxVerifyTimeoutSeconds = 7200
	// maxVerifyEnvNames caps the passthrough env-name list.
	maxVerifyEnvNames = 16
)

// validVerifyEnvName matches a POSIX-style environment variable name.
var validVerifyEnvName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// secretShapedEnvPrefixes and secretShapedEnvSuffixes name env vars that look
// like credentials. Verify env is passthrough names only; denying these keeps
// an operator from routing instance secrets into an agent-run subprocess.
var (
	secretShapedEnvPrefixes = []string{"CM_", "CMX_", "LLM_", "GITHUB_"}
	secretShapedEnvSuffixes = []string{"_TOKEN", "_KEY", "_SECRET", "_PASSWORD"}
)

// validateVerifyConfig screens an operator-declared verify config. It returns a
// descriptive, sentinel-free error on the first violation; callers wrap it with
// the sentinel appropriate to their surface (project vs card) so the API maps
// it to 422. A nil config is valid (nothing declared).
func validateVerifyConfig(v *board.VerifyConfig) error {
	if v == nil {
		return nil
	}

	cmd := strings.TrimSpace(v.Command)
	if len(cmd) > maxVerifyCommandLen {
		return fmt.Errorf("command exceeds %d bytes", maxVerifyCommandLen)
	}

	if strings.ContainsAny(cmd, "\n\r") {
		return errors.New("command must be a single line")
	}

	if strings.ContainsRune(cmd, 0) {
		return errors.New("command must not contain a NUL byte")
	}

	if v.TimeoutSeconds < 0 || v.TimeoutSeconds > maxVerifyTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between 0 and %d", maxVerifyTimeoutSeconds)
	}

	if len(v.Env) > maxVerifyEnvNames {
		return fmt.Errorf("env has %d names, limit is %d", len(v.Env), maxVerifyEnvNames)
	}

	for _, name := range v.Env {
		if !validVerifyEnvName.MatchString(name) {
			return fmt.Errorf("env name %q must match [A-Z_][A-Z0-9_]*", name)
		}

		if isSecretShapedEnvName(name) {
			return fmt.Errorf("env name %q looks secret-shaped; verify env passes names only, never secrets", name)
		}
	}

	return nil
}

// isSecretShapedEnvName reports whether name looks like a credential. The prefix
// check is exact (names are already uppercase per validVerifyEnvName); the
// suffix check is case-insensitive as a belt-and-braces guard.
func isSecretShapedEnvName(name string) bool {
	for _, p := range secretShapedEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	upper := strings.ToUpper(name)
	for _, s := range secretShapedEnvSuffixes {
		if strings.HasSuffix(upper, s) {
			return true
		}
	}

	return false
}

// validateProjectVerify wraps validateVerifyConfig for the project write path so
// a failure surfaces as 422 (board.ErrInvalidProjectConfig), matching the other
// project-config validation failures.
func validateProjectVerify(v *board.VerifyConfig) error {
	if err := validateVerifyConfig(v); err != nil {
		return fmt.Errorf("%w: verify: %v", board.ErrInvalidProjectConfig, err)
	}

	return nil
}

// validateCardVerify wraps validateVerifyConfig for the card write path so a
// failure surfaces as 422 (ErrInvalidVerify).
func validateCardVerify(v *board.VerifyConfig) error {
	if err := validateVerifyConfig(v); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidVerify, err)
	}

	return nil
}

// normalizeVerify returns a fresh, trimmed copy of v, or nil when the result
// carries no operator intent (so .board.yaml / card frontmatter stay clean).
// Never mutates the input.
func normalizeVerify(v *board.VerifyConfig) *board.VerifyConfig {
	if v == nil {
		return nil
	}

	out := board.VerifyConfig{
		Command:        strings.TrimSpace(v.Command),
		TimeoutSeconds: v.TimeoutSeconds,
		// slices.Clone preserves nil vs non-nil-empty: a non-nil empty Env is a
		// card's explicit "override to clear" (drop the project's env), distinct
		// from a nil Env (inherit the project's). board.ResolveVerify honors that
		// distinction, so normalization must not collapse it.
		Env: slices.Clone(v.Env),
	}

	if out.IsZero() {
		return nil
	}

	return &out
}
