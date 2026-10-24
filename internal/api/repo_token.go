package api

import "strings"

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
