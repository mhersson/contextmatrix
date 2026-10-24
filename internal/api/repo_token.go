package api

import (
	"log/slog"
	"strings"
)

// repoNameFromPath returns the repository name from an owner/repo path that
// normalizeWorkerRepoPath has already normalized. Anything but exactly two
// non-empty segments is not ok.
func repoNameFromPath(path string) (string, bool) {
	owner, name, found := strings.Cut(path, "/")
	if !found || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", false
	}

	return name, true
}

// repoNameFromURL returns the bare repository name of a git remote URL, or ""
// when the URL is not an owner/repo remote. Callers pass the result to
// GenerateRepoToken as is: the PAT provider ignores it and the App provider
// rejects "" before any request.
func repoNameFromURL(rawURL string) (string, bool) {
	_, path, ok := workerRepoHostPath(rawURL)
	if !ok {
		return "", false
	}

	return repoNameFromPath(path)
}

// workerDeniedRepoNames turns remote URLs into the lowercased bare repo names
// the worker credentials endpoint refuses. An unparseable URL cannot be
// denied, so it is logged.
func workerDeniedRepoNames(urls []string) map[string]bool {
	out := make(map[string]bool, len(urls))

	for _, u := range urls {
		name, ok := repoNameFromURL(u)
		if !ok {
			slog.Warn("worker git-credentials deny list: skipping unparseable repository URL", "url", redactURLUserinfo(u))

			continue
		}

		out[strings.ToLower(name)] = true
	}

	return out
}

// redactURLUserinfo drops any user:password@ from an http(s)/ssh URL so a
// credential embedded in a configured remote never reaches a log line.
func redactURLUserinfo(raw string) string {
	scheme, rest, found := strings.Cut(raw, "://")
	if !found {
		return raw
	}

	if i := strings.LastIndex(rest, "@"); i >= 0 && !strings.Contains(rest[:i], "/") {
		rest = rest[i+1:]
	}

	return scheme + "://" + rest
}
