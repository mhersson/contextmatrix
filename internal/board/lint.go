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
	{regexp.MustCompile(`(?:/home/|/Users/)[^\s"'` + "`" + `)\]]*`), "absolute path on the card author's machine"},
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
			if form != "" && strings.Contains(lower, strings.ToLower(form)) {
				warnings = append(warnings, fmt.Sprintf(
					"reference to another project's repo %q: the executing agent's container clones only this project's repo - make the card self-contained or move that work to a card on the other project",
					form))

				break // one warning per foreign repo, not per form
			}
		}
	}

	return warnings
}

// repoMatchForms derives the matchable forms of a repo URL: the URL itself
// (with and without a .git suffix) and its owner/name path tail.
func repoMatchForms(repoURL string) []string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil
	}

	bare := strings.TrimSuffix(repoURL, ".git")
	forms := []string{bare}

	// owner/name tail: last two path segments of the URL.
	segs := strings.Split(strings.Trim(bare, "/"), "/")
	if len(segs) >= 2 {
		forms = append(forms, segs[len(segs)-2]+"/"+segs[len(segs)-1])
	}

	return forms
}
