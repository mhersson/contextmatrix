package sentinel

import (
	"strings"
	"unicode"
)

// similarityThreshold is the word 3-shingle Jaccard at which two texts count
// as the same finding.
const similarityThreshold = 0.5

// similar reports whether two texts say the same thing: their word
// 3-shingles overlap enough, or one word sequence contains the other.
func similar(a, b string) bool {
	wa, wb := words(a), words(b)

	if len(wa) < 3 || len(wb) < 3 {
		return strings.Join(wa, " ") == strings.Join(wb, " ")
	}

	sa, sb := shingles(wa), shingles(wb)
	inter := 0

	for s := range sa {
		if sb[s] {
			inter++
		}
	}

	union := len(sa) + len(sb) - inter
	if float64(inter)/float64(union) >= similarityThreshold {
		return true
	}

	ja, jb := " "+strings.Join(wa, " ")+" ", " "+strings.Join(wb, " ")+" "

	return strings.Contains(ja, jb) || strings.Contains(jb, ja)
}

// words lower-cases text and splits it on every run of non-letters and
// non-digits.
func words(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func shingles(w []string) map[string]bool {
	out := make(map[string]bool, len(w))

	for i := 0; i+3 <= len(w); i++ {
		out[strings.Join(w[i:i+3], " ")] = true
	}

	return out
}
