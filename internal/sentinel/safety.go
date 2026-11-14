package sentinel

import (
	"regexp"
	"strings"
)

// minSecretBytes is the shortest secret holdsSecret looks for; shorter ones
// would match ordinary words.
const minSecretBytes = 8

// redactCardIDs replaces each whole-word <PREFIX>-<digits> for a listed
// prefix with "a card". Prefixes are upper-case, so matching is case-sensitive.
func redactCardIDs(text string, prefixes []string) string {
	if len(prefixes) == 0 {
		return text
	}

	quoted := make([]string, len(prefixes))
	for i, p := range prefixes {
		quoted[i] = regexp.QuoteMeta(p)
	}

	re := regexp.MustCompile(`\b(?:` + strings.Join(quoted, "|") + `)-\d+\b`)

	return re.ReplaceAllString(text, "a card")
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
