package api

import "regexp"

// sanitizeErrorDetails returns a short, class-level description of the
// downstream error encoded in raw so the API never leaks filesystem paths,
// remote URLs, or transport auth hints to untrusted callers. Callers are
// expected to log the raw detail before sanitizing so operators still have
// the full context server-side.
//
// The classification cascade is intentional — transport/ssh/exec prefixes are
// checked first because they typically embed host + path. Then we strip any
// go-git ".git/..." segment (the boards-root leak), then any remaining
// absolute-path substring. Anything else is passed through unchanged so
// plain, author-written error messages keep their useful shape.
func sanitizeErrorDetails(raw string) string {
	if raw == "" {
		return ""
	}

	// Transport/ssh/exec classes: these always wrap a remote URL or host,
	// so reply with a stable class label and nothing else.
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
