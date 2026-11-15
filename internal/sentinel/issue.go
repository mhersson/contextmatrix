package sentinel

import (
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

const (
	maxIssueURLBytes = 7500
	trimmedLine      = "_Trimmed to fit GitHub's new-issue link._"
)

// issueContext is what every draft of one routing pass is built with.
type issueContext struct {
	terms    []term
	prefixes []string // every project's card prefix, for redactCardIDs
	version  string   // CM's version; empty writes no version line
}

// issueDraft is the new-issue link an ecosystem finding becomes, or why the
// content check blocks it; it does not check secrets.
func issueDraft(f state.Finding, ic issueContext) (link, blocked string) {
	redact := func(s string) string {
		return redactTerms(redactUsers(redactCardIDs(s, ic.prefixes)), ic.terms)
	}

	// Redact before frictionTitle cuts, so a cut never leaves a fragment of a term.
	f.Title, f.Text, f.Evidence = redact(f.Title), redact(f.Text), redact(f.Evidence)

	title := frictionTitle(f)
	link, body := fitIssue(title, issueBody(f, ic.version))

	if hits := namesTerm(title+"\n"+body, ic.terms); len(hits) > 0 {
		return "", "still names " + strings.Join(hits, ", ")
	}

	return link, ""
}

func issueBody(f state.Finding, version string) string {
	var parts []string

	if version != "" {
		parts = append(parts, "ContextMatrix "+version)
	}

	parts = append(parts, f.Text)

	if f.Evidence != "" {
		parts = append(parts, "## Evidence", f.Evidence)
	}

	return strings.Join(parts, "\n\n")
}

func newIssueURL(title, body string) string {
	q := url.Values{"title": {title}, "body": {body}}

	return "https://github.com/" + upstreamOwner + "/" + upstreamRepo + "/issues/new?" + q.Encode()
}

// fitIssue is the link with the body cut at whitespace to fit, and the body it carries.
func fitIssue(title, body string) (link, fitted string) {
	if link = newIssueURL(title, body); len(link) <= maxIssueURLBytes {
		return link, body
	}

	runes := []rune(body)

	var cuts []int

	for i, r := range runes {
		if unicode.IsSpace(r) {
			cuts = append(cuts, i)
		}
	}

	trimmed := func(cut int) string {
		kept := strings.TrimRightFunc(string(runes[:cut]), unicode.IsSpace)
		if kept == "" {
			return trimmedLine
		}

		return kept + "\n\n" + trimmedLine
	}

	// The link grows with the cut, so the first cut that no longer fits ends the search.
	n := sort.Search(len(cuts), func(i int) bool {
		return len(newIssueURL(title, trimmed(cuts[i]))) > maxIssueURLBytes
	})

	fitted = trimmedLine
	if n > 0 {
		fitted = trimmed(cuts[n-1])
	}

	return newIssueURL(title, fitted), fitted
}
