package sentinel

import (
	"slices"
	"strings"
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

func onlyDraftWords(text string) bool {
	for w := range strings.FieldsFuncSeq(strings.ToLower(text), func(r rune) bool { return !isWordRune(r) }) {
		if !slices.Contains(draftWords, w) {
			return false
		}
	}

	return true
}
