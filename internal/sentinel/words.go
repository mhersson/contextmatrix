package sentinel

import "unicode"

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

func hasWholeWord(text, needle []rune) bool {
	for i := range text {
		if wholeWordAt(text, needle, i) {
			return true
		}
	}

	return false
}
