package sentinel

import (
	"regexp"
	"strings"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

// minSecretBytes is the shortest secret holdsSecret looks for; shorter ones
// would match ordinary words.
const minSecretBytes = 8

// redactCardIDs replaces each whole-word <PREFIX>-<digits> for a listed prefix
// with "a card", and each card-run branch cm/<prefix>-<digits>, its prefix in
// lower case and with any -<word> suffixes, with "a card branch". Matching is
// case-sensitive; an empty prefix is skipped.
func redactCardIDs(text string, prefixes []string) string {
	var ids, branches []string

	for _, p := range prefixes {
		if p != "" {
			ids = append(ids, regexp.QuoteMeta(p))
			branches = append(branches, regexp.QuoteMeta(strings.ToLower(p)))
		}
	}

	if len(ids) == 0 {
		return text
	}

	branch := regexp.MustCompile(`\bcm/(?:` + strings.Join(branches, "|") + `)-\d+(?:-\w+)*\b`)
	id := regexp.MustCompile(`\b(?:` + strings.Join(ids, "|") + `)-\d+\b`)

	return id.ReplaceAllString(branch.ReplaceAllString(text, "a card branch"), "a card")
}

// published is each finding with its text and evidence as Sentinel publishes
// them: card IDs redacted with every project's prefix.
func published(fs []state.Finding, prefixes []string) []state.Finding {
	out := make([]state.Finding, len(fs))

	for i, f := range fs {
		f.Text, f.Evidence = publishedText(f, prefixes), redactCardIDs(f.Evidence, prefixes)
		out[i] = f
	}

	return out
}

// publishedText is a finding's text as the guidance file carries it: card IDs
// redacted with every project's prefix.
func publishedText(f state.Finding, prefixes []string) string {
	return redactCardIDs(f.Text, prefixes)
}

// holdsSecret reports whether text contains any secret of at least
// minSecretBytes bytes.
func holdsSecret(text string, secrets []string) bool {
	for _, s := range secrets {
		if len(s) >= minSecretBytes && strings.Contains(text, s) {
			return true
		}
	}

	return false
}
