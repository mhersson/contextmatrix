package modelcatalog

import (
	"regexp"
	"slices"
	"sort"
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

// creatorOpenAI is the vendor prefix of the one creator whose families take
// the gateway's pinned reasoning effort (llm_endpoint.reasoning_effort).
const creatorOpenAI = "openai"

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

// familyKey reduces an AA slug or a gateway model id to the key both sides
// join on: lowercase, no vendor prefix, dots as dashes, and no trailing
// reasoning-effort suffix or date token. Both AA (gpt-5-2-medium,
// deepseek-v4-flash-0420-high) and vendors (gpt-5.2, claude-sonnet-4-5-20250929)
// name a family this way; nothing else is normalised, so an identity suffix
// (mini, codex, flash, preview) can never be removed.
func familyKey(s string) string {
	key, _, _ := familyKeyParts(s)

	return key
}

// familyKeyParts is familyKey plus what was stripped to reach the key: the
// effort suffixes, outermost first (non-reasoning is one entry), and how
// many date tokens. The family index ranks a row's closeness to a served
// id by these, and matches a wanted reasoning effort against the first
// suffix.
func familyKeyParts(s string) (key string, efforts []string, dateStripped int) {
	s = strings.ToLower(strings.TrimSpace(s))
	if _, name, found := strings.Cut(s, "/"); found {
		s = name
	}

	if s == "" {
		return "", nil, 0
	}

	segs := strings.Split(strings.ReplaceAll(s, ".", "-"), "-")

	for {
		changed := false

		if rest, effort, ok := stripEffort(segs); ok {
			segs, changed = rest, true

			efforts = append(efforts, effort)
		}

		if rest, ok := stripDate(segs); ok {
			segs, changed = rest, true
			dateStripped++
		}

		if !changed {
			break
		}
	}

	return strings.Join(segs, "-"), efforts, dateStripped
}

// effortSuffixes are AA's reasoning-effort variant markers, one segment
// each; non-reasoning spans two segments and is matched first.
var effortSuffixes = map[string]bool{
	"low": true, "medium": true, "high": true, "xhigh": true, "minimal": true,
	"reasoning": true, "thinking": true, "adaptive": true,
}

// stripEffort drops one trailing effort suffix and returns it, keeping at
// least one segment so a bare "high" stays a key.
func stripEffort(segs []string) (rest []string, effort string, ok bool) {
	n := len(segs)

	if n > 2 && segs[n-2] == "non" && segs[n-1] == "reasoning" {
		return segs[:n-2], "non-reasoning", true
	}

	if n > 1 && effortSuffixes[segs[n-1]] {
		return segs[:n-1], segs[n-1], true
	}

	return segs, "", false
}

// dateToken matches the trailing date shapes AA and vendors use, joined by
// dashes: MMDD or YYYYMMDD, MM-DD, MM-YYYY, YYYY-MM-DD, and a month name
// with a two- or four-digit year. Anchored, so every digit group must have
// exactly its length ("4-20" is a version, "001" a revision, "40k" a size),
// and every four-digit year must start with 20, so a version followed by an
// MMDD snapshot ("grok-4-20-0309": "20-0309") is not read as MM-YYYY.
var dateToken = regexp.MustCompile(
	`^(\d{4}|20\d{6}|\d{2}-\d{2}|\d{2}-20\d{2}|20\d{2}-\d{2}-\d{2}|(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*-(\d{2}|20\d{2}))$`)

// stripDate drops one trailing date token of up to three segments, longest
// first so 2024-08-06 is one token rather than 08-06 after 2024. At least
// one segment is kept.
func stripDate(segs []string) ([]string, bool) {
	for take := 3; take >= 1; take-- {
		if len(segs) <= take {
			continue
		}

		if dateToken.MatchString(strings.Join(segs[len(segs)-take:], "-")) {
			return segs[:len(segs)-take], true
		}
	}

	return segs, false
}

// familyKeyOverrides adds index keys for AA slugs whose family key no rule
// reconstructs from the vendor id. AA slug -> extra keys. Ships with
// ContextMatrix; not configuration.
var familyKeyOverrides = map[string][]string{
	"claude-35-sonnet": {"claude-3-5-sonnet"},
	"gpt-35-turbo":     {"gpt-3-5-turbo"},
}

// rewriteKeys returns the additional index keys a family key is reachable
// under. The one rule is Anthropic's ordering flip: AA writes the 4.x
// generation version-first (claude-4-5-sonnet) where the vendor id is
// name-first (claude-sonnet-4-5). A key whose last segment is a word and
// whose segments between "claude" and that word are all numeric also
// indexes with the word moved before the numbers.
func rewriteKeys(key string) []string {
	segs := strings.Split(key, "-")
	if len(segs) < 3 || segs[0] != "claude" {
		return nil
	}

	name := segs[len(segs)-1]
	if !isLetters(name) {
		return nil
	}

	version := segs[1 : len(segs)-1]
	for _, v := range version {
		if !isDigits(v) {
			return nil
		}
	}

	return []string{"claude-" + name + "-" + strings.Join(version, "-")}
}

func isLetters(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}

	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// familyRow is one AA row under a family key with how far its slug is from
// the key: the effort suffixes and date tokens stripped to reach it. Zero on
// both means the slug is the family base row. effort is the first suffix
// stripped (the one the slug ends with), empty for the base row; a wanted
// reasoning effort is matched against it.
type familyRow struct {
	model          aaModel
	effort         string
	effortStripped int
	dateStripped   int
}

// familyIndex is the AA catalog keyed by family key, each key holding every
// row that reduces to it directly, through a rewrite, or through an
// override.
type familyIndex map[string][]familyRow

func indexFamilies(aa []aaModel) familyIndex {
	idx := familyIndex{}

	for _, m := range aa {
		key, efforts, date := familyKeyParts(m.Slug)
		if key == "" {
			continue
		}

		row := familyRow{model: m, effortStripped: len(efforts), dateStripped: date}
		if len(efforts) > 0 {
			row.effort = efforts[0]
		}

		keys := []string{key}
		keys = append(keys, rewriteKeys(key)...)
		keys = append(keys, familyKeyOverrides[m.Slug]...)

		seen := map[string]bool{}
		for _, k := range keys {
			if seen[k] {
				continue
			}

			seen[k] = true
			idx[k] = append(idx[k], row)
		}
	}

	return idx
}

// closest picks the family row to score a served model from. With a wanted
// effort, a scored row carrying exactly that effort suffix beats every
// other row. Otherwise, and among rows that tie on that, the first scored
// row by fewest effort suffixes stripped, then fewest date tokens stripped,
// then highest combined normalized prior. The base row (nothing stripped)
// therefore wins whenever it is scored and no wanted effort names a scored
// sibling, and the strongest sibling is reached only when nothing closer
// is. ok is false when no row in the family is scored, or the key has no
// family.
func (idx familyIndex) closest(key, effort string, maxCoding, maxIntel float64) (aaModel, bool) {
	var (
		best  familyRow
		found bool
	)

	for _, r := range idx[key] {
		if r.model.CodingIndex == nil && r.model.IntelIndex == nil {
			continue
		}

		if !found || closer(r, best, effort, maxCoding, maxIntel) {
			best, found = r, true
		}
	}

	return best.model, found
}

func closer(a, b familyRow, effort string, maxCoding, maxIntel float64) bool {
	if effort != "" && (a.effort == effort) != (b.effort == effort) {
		return a.effort == effort
	}

	if a.effortStripped != b.effortStripped {
		return a.effortStripped < b.effortStripped
	}

	if a.dateStripped != b.dateStripped {
		return a.dateStripped < b.dateStripped
	}

	return combinedPrior(a.model, maxCoding, maxIntel) > combinedPrior(b.model, maxCoding, maxIntel)
}

func combinedPrior(m aaModel, maxCoding, maxIntel float64) float64 {
	return norm(m.CodingIndex, maxCoding) + norm(m.IntelIndex, maxIntel)
}

// creator is the vendor prefix of the family under a key, read from its
// first row: every row that reduces to one key names one model, so they
// share a creator. Empty when the key has no family.
func (idx familyIndex) creator(key string) string {
	rows := idx[key]
	if len(rows) == 0 {
		return ""
	}

	return rows[0].model.Creator
}

// slugs lists every AA slug under a family key, sorted, for the unscored
// exclusion hint.
func (idx familyIndex) slugs(key string) []string {
	rows := idx[key]
	if len(rows) == 0 {
		return nil
	}

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.model.Slug)
	}

	sort.Strings(out)

	return out
}
