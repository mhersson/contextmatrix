package sentinel

import (
	"net"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mhersson/contextmatrix/internal/board"
)

const (
	upstreamOwner = "mhersson"
	upstreamRepo  = "contextmatrix"

	publicHost = "github.com"
)

// ecosystemRepos are the public repositories of the ecosystem, named freely in an issue.
var ecosystemRepos = []string{
	"contextmatrix",
	"contextmatrix-agent",
	"contextmatrix-architect",
	"contextmatrix-backendkit",
	"contextmatrix-chat",
	"contextmatrix-harness",
	"contextmatrix-protocol",
	"contextmatrix-setup",
	"contextmatrix-vendorkit",
}

const (
	wordProject    = "a project"
	wordCard       = "a card"
	wordRepository = "a repository"
	wordHost       = "a host"
)

// genericWords are the replacement words, in the order namesTerm reports them.
var genericWords = []string{wordProject, wordCard, wordRepository, wordHost}

// draftWords are the words a draft writes itself: the replacements, the
// evidence heading and the trim line. A term made only of them never counts
// as a remaining hit.
var draftWords = []string{
	"a", "project", "card", "repository", "host", "branch", "user",
	"evidence", "trimmed", "to", "fit", "github", "s", "new", "issue", "link",
}

// term is one blocklisted text and the generic word that replaces it.
type term struct{ text, word string }

// termSources is everything the blocklist is built from.
type termSources struct {
	Projects  []board.ProjectConfig
	Locations []string // every URL and host the config holds
	RepoURLs  []string // the config's own git remotes
}

// issueTerms builds the blocklist, longest term first.
func issueTerms(src termSources) []term {
	var b termSet

	for i := range src.Projects {
		p := &src.Projects[i]

		b.add(p.Name, wordProject)
		b.add(p.DisplayName, wordProject)
		b.add(p.Prefix, wordCard)

		for _, r := range p.EffectiveRepos() {
			b.addRepo(r.URL)
			b.addRepoName(r.Name)
		}

		if re := p.RemoteExecution; re != nil {
			b.add(imageHost(re.WorkerImage), wordHost)
			b.add(imageHost(re.ChatWorkerImage), wordHost)
		}

		if g := p.GitHub; g != nil {
			b.add(g.Owner, wordRepository)
			b.add(g.Repo, wordRepository)
		}
	}

	for _, u := range src.RepoURLs {
		b.addRepo(u)
	}

	for _, l := range src.Locations {
		b.addLocation(l)
	}

	slices.SortStableFunc(b.terms, func(x, y term) int { return len(y.text) - len(x.text) })

	return b.terms
}

// urlSecrets returns the userinfo password of every URL in src, else, when
// the password is empty, its userinfo user, which a token-as-user remote
// carries. Terms drop them, so the draft check needs them as secrets.
func urlSecrets(src termSources) []string {
	var urls []string

	for i := range src.Projects {
		for _, r := range src.Projects[i].EffectiveRepos() {
			urls = append(urls, r.URL)
		}
	}

	urls = append(urls, src.RepoURLs...)
	urls = append(urls, src.Locations...)

	var out []string

	for _, u := range urls {
		user, password, hasPassword := strings.Cut(parseLocation(u).userinfo, ":")

		secret := user
		if hasPassword && password != "" {
			secret = password
		}

		if secret != "" && !slices.Contains(out, secret) {
			out = append(out, secret)
		}
	}

	return out
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

// termSet collects terms, one per text ignoring case, first kept.
type termSet struct {
	terms []term
	seen  map[string]bool
}

// add trims the runes that are no letter or digit off both ends of text, so a
// name written without its edge punctuation (`Acme Inc.` as `Acme Inc's`)
// still matches; a text left empty is dropped.
func (s *termSet) add(text, word string) {
	text = strings.TrimFunc(text, func(r rune) bool { return !isWordRune(r) })
	key := strings.ToLower(text)

	if text == "" || exempt(key) || s.seen[key] {
		return
	}

	if s.seen == nil {
		s.seen = map[string]bool{}
	}

	s.seen[key] = true
	s.terms = append(s.terms, term{text, word})
}

// addRepoName adds a repository name. A name derived from a URL can carry its
// query or fragment, which may hold a credential, so those are cut.
func (s *termSet) addRepoName(name string) {
	name, _, _ = strings.Cut(name, "#")
	name, _, _ = strings.Cut(name, "?")

	s.add(strings.TrimSuffix(name, ".git"), wordRepository)
}

// addRepo adds a repository URL, every segment of its path and its host.
func (s *termSet) addRepo(raw string) {
	loc := parseLocation(raw)

	if !loc.public() {
		s.add(loc.url, wordRepository)
	}

	for seg := range strings.SplitSeq(loc.path, "/") {
		s.addRepoName(seg)
	}

	s.add(loc.host, wordHost)
}

// addLocation adds a URL or host from the config: the URL when it has a scheme, and its host.
func (s *termSet) addLocation(raw string) {
	loc := parseLocation(raw)

	if loc.scheme != "" && !loc.public() {
		s.add(loc.url, wordHost)
	}

	s.add(loc.host, wordHost)
}

// exempt reports whether a lower-cased text is public: the upstream owner,
// the public host or an ecosystem repository.
func exempt(key string) bool {
	return key == upstreamOwner || key == publicHost || slices.Contains(ecosystemRepos, key)
}

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

// redactTerms replaces every whole-word, case-insensitive hit of each term,
// longest first, with its generic word. A hit becomes a placeholder rune,
// which is no letter or digit and so bounds the next hit, and the placeholders
// expand to their words at the end.
func redactTerms(text string, terms []term) string {
	runes := []rune(text)
	terms = longestFirst(terms)
	holders := map[string]rune{}
	next := '\uE000'

	for _, t := range terms {
		holder, ok := holders[t.word]
		if !ok {
			holder = next
			holders[t.word] = holder
			next++
		}

		runes = replaceWord(runes, []rune(t.text), holder)
	}

	var out strings.Builder

	for _, r := range runes {
		if w := holderWord(holders, r); w != "" {
			out.WriteString(w)
		} else {
			out.WriteRune(r)
		}
	}

	return out.String()
}

// longestFirst returns a copy of terms without the empty ones, longest first
// by rune count, so a shorter term never splits a longer one.
func longestFirst(terms []term) []term {
	out := slices.DeleteFunc(slices.Clone(terms), func(t term) bool { return t.text == "" })
	slices.SortStableFunc(out, func(x, y term) int {
		return utf8.RuneCountInString(y.text) - utf8.RuneCountInString(x.text)
	})

	return out
}

func holderWord(holders map[string]rune, r rune) string {
	for w, h := range holders {
		if h == r {
			return w
		}
	}

	return ""
}

// replaceWord replaces each whole-word, case-insensitive hit of needle with holder.
func replaceWord(text, needle []rune, holder rune) []rune {
	out := make([]rune, 0, len(text))

	for i := 0; i < len(text); {
		if wholeWordAt(text, needle, i) {
			out = append(out, holder)
			i += len(needle)

			continue
		}

		out = append(out, text[i])
		i++
	}

	return out
}

// wholeWordAt reports whether needle sits at text[i:] with no letter or digit on either side.
func wholeWordAt(text, needle []rune, i int) bool {
	return foldAt(text, needle, i) && wordStartAt(text, i) && wordEndAt(text, i+len(needle))
}

// foldAt reports whether needle sits at text[i:], ignoring case.
func foldAt(text, needle []rune, i int) bool {
	if i+len(needle) > len(text) {
		return false
	}

	for j, r := range needle {
		if unicode.ToLower(text[i+j]) != unicode.ToLower(r) {
			return false
		}
	}

	return true
}

func wordStartAt(text []rune, i int) bool { return i == 0 || !isWordRune(text[i-1]) }

func wordEndAt(text []rune, end int) bool { return end == len(text) || !isWordRune(text[end]) }

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// namesTerm returns the generic words of the terms that still hit text, in the
// order project, card, repository, host. A term made only of draftWords is
// skipped: the draft writes those words itself.
func namesTerm(text string, terms []term) []string {
	runes := []rune(text)
	hit := map[string]bool{}

	for _, t := range terms {
		if !onlyDraftWords(t.text) && hasWholeWord(runes, []rune(t.text)) {
			hit[t.word] = true
		}
	}

	var out []string

	for _, w := range genericWords {
		if hit[w] {
			out = append(out, w)
		}
	}

	return out
}

func hasWholeWord(text, needle []rune) bool {
	for i := range text {
		if wholeWordAt(text, needle, i) {
			return true
		}
	}

	return false
}

func onlyDraftWords(text string) bool {
	for w := range strings.FieldsFuncSeq(strings.ToLower(text), func(r rune) bool { return !isWordRune(r) }) {
		if !slices.Contains(draftWords, w) {
			return false
		}
	}

	return true
}
