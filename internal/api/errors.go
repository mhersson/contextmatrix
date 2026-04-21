package api

import (
	encodingjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// sanitizeErrorDetails converts an error into a short, sanitized detail
// string suitable for inclusion in a client-facing error response body.
//
// JSON-decoder errors are mapped to stable, descriptive messages first.
// For other errors the underlying string is run through a regex cascade
// that strips filesystem paths, go-git transport hints, and exec leaks
// before being returned. Anything that doesn't match a sanitization rule is
// returned unchanged so plain author-written error text keeps its shape.
//
// Callers should log the raw error before sanitizing so operators retain
// the full context server-side.
func sanitizeErrorDetails(err error) string {
	if err == nil {
		return ""
	}

	// Typed JSON-decoder errors first: stdlib stable phrasing, no leakage.
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

	raw := err.Error()

	// Catch-all for remaining JSON decoder errors that don't match a typed
	// error.
	if strings.HasPrefix(raw, "json: ") {
		return "invalid JSON body"
	}

	// Transport / ssh / exec classes always wrap a remote URL or host —
	// reply with a stable class label and nothing else.
	if transportPrefixRe.MatchString(raw) {
		return "git remote unreachable"
	}

	if execPrefixRe.MatchString(raw) {
		return "git operation failed"
	}

	// go-git ".git/..." path leak (boards-dir, worktree root, etc).
	if gitDirPathRe.MatchString(raw) {
		return "git operation failed"
	}

	// Generic absolute-path leak (os.PathError, anything filesystem-ish).
	if absPathRe.MatchString(raw) {
		return "filesystem error"
	}

	return raw
}

// transportPrefixRe matches go-git transport errors (e.g.
// "transport: authentication required") and raw ssh: errors. Anchored so it
// only fires when the raw string starts with one of these tokens or is
// prefixed by a wrapping operation name.
var transportPrefixRe = regexp.MustCompile(`(?:^|: )(?:transport\.|transport: |ssh:)`)

// execPrefixRe matches os/exec errors (e.g. "exec: \"git\": executable file
// not found in $PATH").
var execPrefixRe = regexp.MustCompile(`(?:^|: )exec: `)

// gitDirPathRe matches any substring that looks like an absolute path passing
// through a .git directory (e.g. "/var/lib/contextmatrix/boards/.git/refs" or
// "/home/alice/boards/project/.git").
var gitDirPathRe = regexp.MustCompile(`/[A-Za-z0-9_.\-/]+/\.git(?:/|$| )`)

// absPathRe matches any substring that looks like an absolute POSIX path with
// at least one directory component and a file/dir name after (e.g.
// "/home/alice/boards/project", "/tmp/fooBar"). Intentionally conservative —
// catches leaks like "open /data/boards/project/tasks/CARD-001.md: ..."
// without false-positiving on short ids.
var absPathRe = regexp.MustCompile(`/[A-Za-z0-9_.\-/]+/[A-Za-z0-9_.\-]+`)
