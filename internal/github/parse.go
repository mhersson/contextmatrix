package github

import "github.com/mhersson/contextmatrix/internal/githuburl"

// ParseGitHubRepo extracts owner, repo and matched host from a GitHub URL.
// Thin wrapper over githuburl.Parse so service-layer callers that cannot
// import this package (it depends on internal/service) share one parser.
func ParseGitHubRepo(rawURL string, allowedHosts []string) (owner, repo, host string, ok bool) {
	return githuburl.Parse(rawURL, allowedHosts)
}
