package sentinel

import (
	"net"
	"slices"
	"strings"
)

// location is a URL or host split into the parts the blocklist uses.
type location struct {
	scheme string
	host   string // no port
	path   string // no leading or trailing slash
	url    string // no userinfo, query or fragment

	userinfo string
}

// parseLocation splits a URL, an scp-style remote (user@host:owner/name) or a
// bare host. It never fails: the credentials are cut by hand so a malformed
// value cannot leak them into a term.
func parseLocation(raw string) location {
	s := strings.TrimSpace(raw)

	var loc location

	if i := strings.Index(s, "://"); i >= 0 {
		loc.scheme, s = s[:i], s[i+3:]
	}

	s, _, _ = strings.Cut(s, "#")
	s, _, _ = strings.Cut(s, "?")

	userinfo, rest := splitUserinfo(s)

	var authority, path string

	sep := "/"

	if c := scpColon(rest); loc.scheme == "" && c >= 0 {
		authority, path, sep = rest[:c], rest[c+1:], ":"
	} else {
		authority, path, _ = strings.Cut(rest, "/")
	}

	loc.userinfo = userinfo
	loc.path = strings.Trim(path, "/")
	loc.host = authority

	if h, _, err := net.SplitHostPort(authority); err == nil {
		loc.host = h
	}

	loc.host = strings.Trim(loc.host, "[]")

	loc.url = authority
	if loc.scheme != "" {
		loc.url = loc.scheme + "://" + authority
	}

	if loc.path != "" {
		loc.url += sep + loc.path
	}

	return loc
}

// splitUserinfo cuts the userinfo off the front of s, looking only before the
// first slash.
func splitUserinfo(s string) (userinfo, rest string) {
	head, _, _ := strings.Cut(s, "/")

	at := strings.LastIndex(head, "@")
	if at < 0 {
		return "", s
	}

	return s[:at], s[at+1:]
}

// scpColon returns the index of the colon that makes s an scp-style remote
// (git's rule: a colon before the first slash), or -1. A bracketed IPv6 host
// and a host:port are not scp.
func scpColon(s string) int {
	head, _, _ := strings.Cut(s, "/")
	c := strings.Index(head, ":")

	if c < 0 || strings.HasPrefix(s, "[") || isPort(head[c+1:]) {
		return -1
	}

	return c
}

func isPort(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}

// public reports whether the location is on the public host with no path or
// an ecosystem repository's path, so naming it leaks nothing.
func (l location) public() bool {
	if !strings.EqualFold(l.host, publicHost) {
		return false
	}

	owner, repo, _ := strings.Cut(strings.TrimSuffix(l.path, ".git"), "/")

	return l.path == "" || (strings.EqualFold(owner, upstreamOwner) && slices.Contains(ecosystemRepos, strings.ToLower(repo)))
}

// imageHost returns the registry host of an image reference by Docker's rule:
// the first component when the reference has a slash and that component holds
// a dot or a colon or is localhost. The port is dropped.
func imageHost(ref string) string {
	first, _, found := strings.Cut(strings.TrimSpace(ref), "/")

	if !found || !strings.ContainsAny(first, ".:") && first != "localhost" {
		return ""
	}

	host, _, _ := strings.Cut(first, ":")

	return host
}
