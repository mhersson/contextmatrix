package modelcatalog

import (
	"regexp"
	"slices"
	"strings"
)

// aaCreatorNameToOR maps an AA model_creator.name to the OpenRouter vendor
// prefix, for creators whose slugified name diverges from it. The AA free v2
// API dropped model_creator.slug, so the vendor prefix is the single creator
// vocabulary: allowlists, CandidateModel.Creator, and the OR slug join all
// speak it. Names absent here fall back to slugifyCreator.
var aaCreatorNameToOR = map[string]string{
	"Alibaba":  "qwen",
	"Kimi":     "moonshotai",
	"SpaceXAI": "x-ai",
}

// creatorNonAlnum matches the character runs slugifyCreator collapses.
var creatorNonAlnum = regexp.MustCompile("[^a-z0-9]+")

func slugifyCreator(name string) string {
	return strings.Trim(creatorNonAlnum.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// creatorSlug resolves an AA creator name to the OR vendor prefix. Unknown
// names get a stable mechanical identity so vendor diversity and operator
// allowlists still work for creators AA adds later.
func creatorSlug(name string) string {
	name = strings.TrimSpace(name)
	if prefix, ok := aaCreatorNameToOR[name]; ok {
		return prefix
	}

	return slugifyCreator(name)
}

// aaSlugOverrides handles version-ambiguous AA slugs the heuristic cannot
// reconstruct. AA slug -> full OR slug.
var aaSlugOverrides = map[string]string{
	"mistral-large-2512": "mistralai/mistral-large-2512",
}

// versionDash matches a digit-dash-digit run so "5-2" -> "5.2", "k2-7" -> "k2.7".
var versionDash = regexp.MustCompile(`(\d)-(\d)`)

// mapAASlug converts an AA (slug, creator) to a full OpenRouter slug. The
// creator is already the OR vendor prefix (see creatorSlug); ok=false only
// when it is empty (caller logs + skips).
func mapAASlug(aaSlug, aaCreator string) (string, bool) {
	if full, ok := aaSlugOverrides[aaSlug]; ok {
		return full, true
	}

	if aaCreator == "" {
		return "", false
	}

	name := aaSlug
	for versionDash.MatchString(name) {
		name = versionDash.ReplaceAllString(name, "$1.$2")
	}

	return aaCreator + "/" + name, true
}

// trustedCreators is the allowlist of creator vendor prefixes eligible for
// auto-selection. Overridable via config (see Builder.Allowlist).
var trustedCreators = []string{
	"openai", "anthropic", "google", "deepseek",
	"z-ai", "moonshotai", "minimax", "x-ai",
}

func isTrusted(creator string, allow []string) bool {
	if len(allow) == 0 {
		allow = trustedCreators
	}

	return strings.TrimSpace(creator) != "" && slices.Contains(allow, creator)
}

// allowedORPrefixes returns the effective creator allowlist (config override
// or built-in trustedCreators) as a set. Allowlist entries are OR vendor
// prefixes already.
func allowedORPrefixes(allow []string) map[string]bool {
	if len(allow) == 0 {
		allow = trustedCreators
	}

	out := make(map[string]bool, len(allow))
	for _, c := range allow {
		out[c] = true
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
