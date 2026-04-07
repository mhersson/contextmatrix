package github

import (
	"net/url"
	"strings"
)

// ParseGitHubRepo extracts owner and repo from a GitHub URL.
// Supported formats:
//   - git@github.com:owner/repo.git
//   - https://github.com/owner/repo.git
//   - https://github.com/owner/repo
//
// Returns empty strings and false if the URL is not a GitHub URL or cannot be parsed.
func ParseGitHubRepo(rawURL string) (owner, repo string, ok bool) {
	// SSH format: git@github.com:owner/repo.git
	if path, ok := strings.CutPrefix(rawURL, "git@github.com:"); ok {
		path = strings.TrimSuffix(path, ".git")
		return splitOwnerRepo(path)
	}

	// HTTPS format
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", false
	}

	if u.Hostname() != "github.com" {
		return "", "", false
	}

	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	return splitOwnerRepo(path)
}

func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
