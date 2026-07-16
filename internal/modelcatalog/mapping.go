package modelcatalog

import (
	"regexp"
	"slices"
	"strings"
)

// aaCreatorToOR maps an AA model_creator.slug to the OpenRouter namespace
// prefix. Creators absent here are used verbatim.
var aaCreatorToOR = map[string]string{
	"zai":       "z-ai",
	"alibaba":   "qwen",
	"openai":    "openai",
	"anthropic": "anthropic",
	"google":    "google",
	"deepseek":  "deepseek",
	"minimax":   "minimax",
	"x-ai":      "x-ai",
}

// aaSlugOverrides handles version-ambiguous AA slugs the heuristic cannot
// reconstruct. AA slug -> full OR slug.
var aaSlugOverrides = map[string]string{
	"mistral-large-2512": "mistralai/mistral-large-2512",
}

// versionDash matches a digit-dash-digit run so "5-2" -> "5.2", "k2-7" -> "k2.7".
var versionDash = regexp.MustCompile(`(\d)-(\d)`)

// mapAASlug converts an AA (slug, creator) to a full OpenRouter slug. Returns
// ok=false when the creator is unknown (caller logs + skips).
func mapAASlug(aaSlug, aaCreator string) (string, bool) {
	if full, ok := aaSlugOverrides[aaSlug]; ok {
		return full, true
	}

	prefix, ok := aaCreatorToOR[aaCreator]
	if !ok {
		return "", false
	}

	name := aaSlug
	for versionDash.MatchString(name) {
		name = versionDash.ReplaceAllString(name, "$1.$2")
	}

	return prefix + "/" + name, true
}

// trustedCreators is the allowlist of AA creator slugs eligible for
// auto-selection. Overridable via config (see Builder.Allowlist).
var trustedCreators = []string{
	"openai", "anthropic", "google", "deepseek", "alibaba",
	"zai", "minimax", "x-ai",
}

func isTrusted(creator string, allow []string) bool {
	if len(allow) == 0 {
		allow = trustedCreators
	}

	return strings.TrimSpace(creator) != "" && slices.Contains(allow, creator)
}

// allowedORPrefixes maps the effective creator allowlist (config override or
// built-in trustedCreators) to OpenRouter namespace prefixes.
func allowedORPrefixes(allow []string) map[string]bool {
	if len(allow) == 0 {
		allow = trustedCreators
	}

	out := make(map[string]bool, len(allow))

	for _, c := range allow {
		if p, ok := aaCreatorToOR[c]; ok {
			out[p] = true

			continue
		}

		out[c] = true // creators absent from the map are used verbatim
	}

	return out
}

// servedSlugAllowed reports whether an OR slug passes the vendor screen: its
// vendor prefix is allowlisted, it is an operator favorite, or it is the
// openrouter/auto router (kept pinnable by design).
func servedSlugAllowed(slug string, allowed, favorites map[string]bool) bool {
	if slug == "openrouter/auto" || favorites[slug] {
		return true
	}

	vendor, _, ok := strings.Cut(slug, "/")

	return ok && allowed[vendor]
}
