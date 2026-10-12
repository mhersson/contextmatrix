package jira

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var issueKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[1-9][0-9]*$`)

// IsIssueKey reports whether s is a Jira issue key such as PROJ-123.
func IsIssueKey(s string) bool {
	return issueKeyPattern.MatchString(s)
}

// ParseIssueRef returns the issue key named by input: a key in any case, a
// /browse/KEY URL on the configured site, or a site URL carrying
// selectedIssue=KEY. URLs on any other host are rejected so a pasted link can
// never point the client at a foreign site.
func ParseIssueRef(input, siteURL string) (string, error) {
	in := strings.TrimSpace(input)

	if key := strings.ToUpper(in); IsIssueKey(key) {
		return key, nil
	}

	u, err := url.Parse(in)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, input)
	}

	site, err := url.Parse(strings.TrimSpace(siteURL))
	if err != nil || site.Host == "" || !strings.EqualFold(u.Host, site.Host) {
		return "", fmt.Errorf("%w: %q is not on the configured Jira site", ErrInvalidRef, input)
	}

	if rest, ok := strings.CutPrefix(u.Path, "/browse/"); ok {
		if key := strings.ToUpper(strings.TrimSuffix(rest, "/")); IsIssueKey(key) {
			return key, nil
		}
	}

	if key := strings.ToUpper(u.Query().Get("selectedIssue")); IsIssueKey(key) {
		return key, nil
	}

	return "", fmt.Errorf("%w: %q names no issue", ErrInvalidRef, input)
}
