package github

import (
	"net/url"
	"strings"
)

// ParseGitHubRepo extracts owner, repo, and matched host from a GitHub URL.
// Supported formats:
//   - git@<host>:owner/repo.git
//   - https://<host>/owner/repo.git
//   - https://<host>/owner/repo
//   - ssh://<host>/owner/repo.git
//
// allowedHosts is the list of permitted hostnames (e.g. ["github.com", "acme.ghe.com"]).
// Returns empty strings and false if the URL does not match any allowed host or cannot be parsed.
func ParseGitHubRepo(rawURL string, allowedHosts []string) (owner, repo, host string, ok bool) {
	// SSH SCP format: git@<host>:owner/repo.git
	for _, h := range allowedHosts {
		prefix := "git@" + h + ":"
		if path, matched := strings.CutPrefix(rawURL, prefix); matched {
			path = strings.TrimSuffix(path, ".git")

			o, r, valid := splitOwnerRepo(path)
			if valid {
				return o, r, h, true
			}

			return "", "", "", false
		}
	}

	// HTTPS / SSH URL format
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", false
	}

	hostname := u.Hostname()
	for _, h := range allowedHosts {
		if hostname == h {
			path := strings.TrimPrefix(u.Path, "/")
			path = strings.TrimSuffix(path, ".git")

			o, r, valid := splitOwnerRepo(path)
			if valid {
				return o, r, h, true
			}

			return "", "", "", false
		}
	}

	return "", "", "", false
}

func splitOwnerRepo(path string) (owner, repo string, ok bool) {
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}

	return parts[0], parts[1], true
}
