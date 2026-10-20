package board

import (
	"fmt"
	"regexp"
	"strings"
)

// selfContainmentPatterns are the deterministic signals that a card body
// references the author's environment rather than the project repo. Each match
// produces one advisory warning; the lint never blocks a mutation.
var selfContainmentPatterns = []struct {
	re     *regexp.Regexp
	reason string
}{
	{regexp.MustCompile(`(?:^|[\s"'` + "`" + `(\[])(?:/home/|/Users/)[^\s"'` + "`" + `)\]]*`), "absolute path on the card author's machine"},
	{regexp.MustCompile(`(?:^|[\s"'` + "`" + `(\[])~/[^\s"'` + "`" + `)\]]*`), "home-relative path on the card author's machine"},
	{regexp.MustCompile(`(?i)\b[a-z]:\\[^\s"'` + "`" + `)\]]*`), "Windows path on the card author's machine"},
	{regexp.MustCompile(`file://[^\s"'` + "`" + `)\]]*`), "file:// URL"},
}

// LintSelfContained scans card text for references the executing agent's
// container cannot reach: local filesystem paths, file:// URLs, and mentions
// of other projects' repos. foreignRepos holds the repo URLs of every OTHER
// project; both the full URL and its owner/name path form are matched.
// Returns one warning per finding; empty when clean. Advisory only.
func LintSelfContained(text string, foreignRepos []string) []string {
	var warnings []string

	text = withoutMockupBlocks(text)

	for _, p := range selfContainmentPatterns {
		for _, m := range p.re.FindAllString(text, -1) {
			snippet := strings.TrimSpace(m)
			warnings = append(warnings, fmt.Sprintf(
				"%s %q: the executing agent runs in a container holding only a fresh clone of this project's repo - inline the needed content or reference an in-repo path instead",
				p.reason, snippet))
		}
	}

	lower := strings.ToLower(text)

	for _, repo := range foreignRepos {
		for _, form := range repoMatchForms(repo) {
			if form != "" && hasMatchWithBoundary(lower, strings.ToLower(form)) {
				warnings = append(warnings, fmt.Sprintf(
					"reference to another project's repo %q: the executing agent's container clones only this project's repo - make the card self-contained or move that work to a card on the other project",
					form))

				break // one warning per foreign repo, not per form
			}
		}
	}

	return warnings
}

var mockupOpen = regexp.MustCompile("^(`{3,})mockup[ \\t]*$")

// withoutMockupBlocks drops fenced mockup blocks, fences included. A mockup
// imitates screens, so sample paths in it are not the author's environment.
// The grammar is shared with the architect's contract check and the agent's
// card classifier: backtick fences at column 0, closed by a backtick-only line
// at least as long as the opening, unclosed runs to the end.
func withoutMockupBlocks(text string) string {
	var (
		out     strings.Builder
		fence   int
		inBlock bool
	)

	for _, line := range strings.SplitAfter(text, "\n") {
		bare := strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")

		if !inBlock {
			if m := mockupOpen.FindStringSubmatch(bare); m != nil {
				inBlock, fence = true, len(m[1])

				continue
			}

			out.WriteString(line)

			continue
		}

		if closesMockup(bare, fence) {
			inBlock = false
		}
	}

	return out.String()
}

func closesMockup(line string, fence int) bool {
	ticks := strings.TrimRight(line, " \t")

	return len(ticks) >= fence && strings.Trim(ticks, "`") == ""
}

// repoMatchForms derives the matchable forms of a repo URL: the URL itself
// (with and without a .git suffix) and its owner/name path tail. Handles both
// HTTPS (github.com/owner/repo) and SSH (git@github.com:owner/repo) URLs.
func repoMatchForms(repoURL string) []string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil
	}

	bare := strings.TrimSuffix(repoURL, ".git")
	forms := []string{bare}

	// owner/name tail: last two path segments, using both / and : as separators
	// to handle both HTTPS (github.com/owner/repo) and SSH (git@github.com:owner/repo).
	trimmed := strings.Trim(bare, "/")
	if idx := strings.LastIndexAny(trimmed, "/:"); idx >= 0 {
		rest := trimmed[idx+1:]
		if idx2 := strings.LastIndexAny(trimmed[:idx], "/:"); idx2 >= 0 {
			owner := trimmed[idx2+1 : idx]
			if owner != "" && rest != "" {
				forms = append(forms, owner+"/"+rest)
			}
		}
	}

	return forms
}

// hasMatchWithBoundary reports whether needle appears in haystack with word/path
// boundaries on both sides: the character before and after the match must be
// absent or not in [A-Za-z0-9_-].
func hasMatchWithBoundary(haystack, needle string) bool {
	idx := 0
	for {
		pos := strings.Index(haystack[idx:], needle)
		if pos < 0 {
			return false
		}

		actualPos := idx + pos

		// Check boundary before the match
		if actualPos > 0 {
			before := haystack[actualPos-1]
			if isWordPathChar(before) {
				idx = actualPos + 1

				continue
			}
		}

		// Check boundary after the match
		endPos := actualPos + len(needle)
		if endPos < len(haystack) {
			after := haystack[endPos]
			if isWordPathChar(after) {
				idx = actualPos + 1

				continue
			}
		}

		return true
	}
}

func isWordPathChar(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') || b == '_' || b == '-'
}
