package modelcatalog

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// refreshFailureCooldown is how long the Builder waits after a refresh attempt
// before allowing another one once the cache is stale. Between attempts,
// callers are served the last-good snapshot (or nothing if no refresh has ever
// succeeded). Without this, every Rate/Candidates call during a catalog
// provider outage would re-attempt the fetch and eat the full timeout budget
// (a 30s catalog request plus the aaFetchBudget-bounded AA fetch) per call.
const refreshFailureCooldown = 60 * time.Second

// Builder fetches AA + OR on a TTL and produces the candidate set. Safe for
// concurrent use; serves the last-good snapshot when a refresh fails.
type Builder struct {
	aaEndpoint, orEndpoint, aaKey string
	floor                         float64
	allowlist                     []string
	ttl                           time.Duration

	// Endpoint leg (openai type). When endpointBaseURL != "", refresh() fuses
	// the endpoint catalog with AA priors via aaModelMap/priors instead of the OR leg.
	endpointBaseURL string
	endpointAPIKey  string
	aaModelMap      map[string]string
	priors          map[string]PriorOverride

	// favorites are operator-configured slugs (flattened across tiers/roles).
	// They pass the Served() vendor screen even when their vendor is not
	// allowlisted - the operator explicitly trusts them.
	favorites []string

	mu       sync.Mutex
	cached   []protocol.CandidateModel
	cachedAt time.Time
	// lastCatalog is the raw per-slug catalog from the most recent refresh (every
	// served model, not just selection candidates). Guarded by mu; consumed by
	// Rate() for per-slug cost lookups.
	lastCatalog map[string]orEntry
	// lastRefreshAttempt is when refresh() was last invoked, success or
	// failure. Guarded by mu. Gates re-attempts on a stale cache so a failing
	// provider is retried at most once per refreshFailureCooldown.
	lastRefreshAttempt time.Time
}

// BuilderOption configures a Builder after construction.
type BuilderOption func(*Builder)

// WithEndpoint switches the Builder to the openai endpoint leg: it fetches the
// endpoint's /v1/models (authenticated) and fuses with AA priors via aaModelMap,
// with per-slug operator overrides from priors.
func WithEndpoint(baseURL, apiKey string, aaModelMap map[string]string, priors map[string]PriorOverride) BuilderOption {
	return func(b *Builder) {
		b.endpointBaseURL = baseURL
		b.endpointAPIKey = apiKey
		b.aaModelMap = aaModelMap
		b.priors = priors
	}
}

// WithFavorites registers operator-configured favorite slugs; they pass the
// Served() vendor screen regardless of vendor.
func WithFavorites(favs []string) BuilderOption {
	return func(b *Builder) { b.favorites = favs }
}

// NewBuilder constructs a Builder. floor<=0 defaults to 0.65; ttl<=0 to 6h.
func NewBuilder(aaKey string, floor float64, allowlist []string, ttl time.Duration, opts ...BuilderOption) *Builder {
	if floor <= 0 {
		floor = 0.65
	}

	if ttl <= 0 {
		ttl = 6 * time.Hour
	}

	b := &Builder{
		aaEndpoint: AADefaultEndpoint, orEndpoint: ORDefaultEndpoint,
		aaKey: aaKey, floor: floor, allowlist: allowlist, ttl: ttl,
	}

	for _, opt := range opts {
		opt(b)
	}

	return b
}

// refreshIfStaleLocked checks whether the cache is stale and refreshes it if
// so. Must be called with b.mu held. On refresh failure it logs, leaves
// b.cached unchanged (last-good), and backs off: no re-attempt happens until
// refreshFailureCooldown has elapsed since the last attempt, so a provider
// outage costs at most one fetch per cooldown window instead of one per call.
// On success it updates b.cached, b.cachedAt, and b.lastCatalog (via
// b.refresh). Note: a successful refresh also stamps lastRefreshAttempt, which
// is harmless because the TTL check short-circuits until expiry and every
// realistic TTL far exceeds the cooldown.
func (b *Builder) refreshIfStaleLocked(ctx context.Context) {
	if b.cached != nil && time.Since(b.cachedAt) < b.ttl {
		return
	}

	if time.Since(b.lastRefreshAttempt) < refreshFailureCooldown {
		return
	}

	b.lastRefreshAttempt = time.Now()

	fresh, err := b.refresh(ctx)
	if err != nil {
		slog.Warn("model catalog refresh failed; using last-good and backing off",
			"error", err, "have", b.cached != nil, "cooldown", refreshFailureCooldown)

		return
	}

	b.cached, b.cachedAt = fresh, time.Now()
}

// Candidates returns the current candidate set, refreshing if the cache is
// stale. On refresh failure it logs and returns the last-good snapshot (nil
// only if no successful build has ever happened).
//
// A nil receiver yields nil (no candidates) without panicking - this handles
// the typed-nil-interface case where a nil *Builder is boxed into a
// catalogProvider interface value before the caller's nil check runs.
func (b *Builder) Candidates(ctx context.Context) []protocol.CandidateModel {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return b.cached
}

// ModelPrice is the per-token price set for one served model. CacheRead and
// CacheWrite are zero when the gateway publishes no cache pricing; callers
// fall back to multiplier-derived rates.
type ModelPrice struct {
	Prompt, Completion, CacheRead, CacheWrite float64
}

// Rate returns the per-token price set for slug from the most recent raw
// catalog (every served model, refreshing if stale). ok is false when the
// slug is not served. Unlike Candidates, this is not filtered to
// AA-rated/floor-clearing models, so picker-only and below-floor models are
// still priced.
func (b *Builder) Rate(ctx context.Context, slug string) (ModelPrice, bool) {
	if b == nil {
		return ModelPrice{}, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	e, found := b.lastCatalog[slug]
	if !found {
		return ModelPrice{}, false
	}

	return ModelPrice{
		Prompt:     e.PromptPrice,
		Completion: e.CompletionPrice,
		CacheRead:  e.CacheReadPrice,
		CacheWrite: e.CacheWritePrice,
	}, true
}

// ServedModel is one entry of the picker/validation model set.
type ServedModel struct {
	Slug          string
	ContextWindow int
}

// Served returns the picker/validation model set, refreshing if stale. On the
// OpenRouter leg the raw catalog is vendor-screened (allowlist prefixes, plus
// openrouter/auto and operator favorites); the endpoint leg is served
// unfiltered because the operator already curates it. Sorted by slug. Nil on
// a nil receiver or when no catalog has ever been fetched.
//
// Like Rate, a stale cache triggers a synchronous network refresh under b.mu.
// Callers on write paths (card-pin validation via Validate) accept this
// bounded stall: at most one fetch per TTL, or one per refreshFailureCooldown
// during a provider outage.
func (b *Builder) Served(ctx context.Context) []ServedModel {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	if len(b.lastCatalog) == 0 {
		return nil
	}

	screen := b.endpointBaseURL == ""

	var allowed map[string]bool
	if screen {
		allowed = allowedORPrefixes(b.allowlist)
	}

	favs := make(map[string]bool, len(b.favorites))
	for _, f := range b.favorites {
		favs[f] = true
	}

	out := make([]ServedModel, 0, len(b.lastCatalog))

	for slug, e := range b.lastCatalog {
		if screen && !servedSlugAllowed(slug, allowed, favs) {
			continue
		}

		out = append(out, ServedModel{Slug: slug, ContextWindow: e.ContextWindow})
	}

	if len(out) == 0 {
		return nil
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })

	return out
}

// Validate reports whether slug is in the served model set. Fail-open: returns
// true on a nil receiver or when the catalog is empty/never fetched, so an
// OpenRouter/AA outage or cold start never blocks work.
func (b *Builder) Validate(ctx context.Context, slug string) bool {
	served := b.Served(ctx)
	if len(served) == 0 {
		return true
	}

	for _, m := range served {
		if m.Slug == slug {
			return true
		}
	}

	return false
}

func (b *Builder) refresh(ctx context.Context) ([]protocol.CandidateModel, error) {
	// Endpoint leg (openai type): pricing comes from the endpoint's own /models
	// and is independent of Artificial Analysis, so fetch it whenever configured
	// - even without an AA key. This lets a chat-only deployment (no agent
	// backend, no AA key) still price endpoint-served models via Rate().
	if b.endpointBaseURL != "" {
		ep, err := fetchEndpointCatalog(ctx, b.endpointBaseURL, b.endpointAPIKey)
		if err != nil {
			return nil, err
		}

		b.lastCatalog = ep

		// Without an AA key there are no selection candidates (the complexity
		// selector is an agent-only concern), but per-slug pricing is populated.
		if b.aaKey == "" {
			return []protocol.CandidateModel{}, nil
		}

		aa, err := fetchAAModels(ctx, b.aaEndpoint, b.aaKey)
		if err != nil {
			return nil, err
		}

		built, exclusions := buildFromAAMap(aa, ep, b.aaModelMap, b.priors, b.floor)

		// Deterministic audit trail: buildFromAAMap iterates the endpoint map,
		// so both lists arrive in map order. Sort by slug so refresh-to-refresh
		// logs are diffable (Served() sorts the same way).
		slices.SortFunc(exclusions, func(a, c aaExclusion) int { return strings.Compare(a.Slug, c.Slug) })
		slices.SortFunc(built, func(a, c aaScored) int { return strings.Compare(a.Candidate.Slug, c.Candidate.Slug) })

		// "Served but unselectable" is a loud condition, not a silent one: a
		// tool-capable served model that yields no candidate (unmapped, mapped
		// to a missing AA slug, unscored, or below the floor) means selection
		// will fall back to the default model for that quality. One WARN per
		// excluded model, naming the slug and the specific reason.
		for _, x := range exclusions {
			if len(x.Siblings) > 0 {
				slog.Warn("endpoint model not selectable", "slug", x.Slug, "reason", x.Reason,
					"siblings", formatSiblings(x.Siblings))
			} else {
				slog.Warn("endpoint model not selectable", "slug", x.Slug, "reason", x.Reason)
			}
		}

		// Resolved candidate set: one line per served candidate with its priors
		// and where they came from, so the operator can audit the join.
		for _, s := range built {
			slog.Info("endpoint model scored",
				"slug", s.Candidate.Slug, "coder_prior", s.Candidate.CoderPrior,
				"reviewer_prior", s.Candidate.ReviewerPrior, "source", s.Source)
		}

		cands := make([]protocol.CandidateModel, 0, len(built))
		for _, s := range built {
			cands = append(cands, s.Candidate)
		}

		return cands, nil
	}

	// OpenRouter leg: the OR catalog is public and unauthenticated - fetch it
	// even without an AA key so Rate/Served/Validate work on AA-less
	// deployments. Candidates still require AA to normalize prior indices.
	or, err := fetchORCatalog(ctx, b.orEndpoint)
	if err != nil {
		return nil, err
	}

	b.lastCatalog = or

	if b.aaKey == "" {
		return []protocol.CandidateModel{}, nil
	}

	aa, err := fetchAAModels(ctx, b.aaEndpoint, b.aaKey)
	if err != nil {
		return nil, err
	}

	return build(aa, or, b.floor, b.allowlist), nil
}

// build is the pure transform: normalize indices against the response-wide
// max, keep trusted-creator models clearing the floor for at least one role,
// map to OR, collapse effort variants (same OR slug -> highest prior), join
// price/window/tools. Effort collapse falls out of keying by OR slug.
func build(aa []aaModel, or map[string]orEntry, floor float64, allow []string) []protocol.CandidateModel {
	maxCoding, maxIntel := maxIndices(aa)

	if maxCoding <= 0 || maxIntel <= 0 {
		return []protocol.CandidateModel{}
	}

	byOR := map[string]protocol.CandidateModel{}

	for _, m := range aa {
		if !isTrusted(m.Creator, allow) {
			continue
		}

		coder := norm(m.CodingIndex, maxCoding)

		rev := norm(m.IntelIndex, maxIntel)
		if coder < floor && rev < floor { // below floor for every role
			continue
		}

		orSlug, ok := mapAASlug(m.Slug, m.Creator)
		if !ok {
			slog.Debug("unmapped AA model skipped", "slug", m.Slug, "creator", m.Creator)

			continue
		}

		e, ok := or[orSlug]
		if !ok || !e.Tools {
			continue // not on OR, or not tool-capable
		}

		cand := protocol.CandidateModel{
			Slug:                  orSlug,
			PromptPricePerTok:     e.PromptPrice,
			CompletionPricePerTok: e.CompletionPrice,
			ContextWindow:         e.ContextWindow,
			CoderPrior:            coder,
			ReviewerPrior:         rev,
			Creator:               m.Creator,
		}
		// Effort-variant collapse: keep the strongest per OR slug.
		if prev, exists := byOR[orSlug]; !exists ||
			cand.CoderPrior+cand.ReviewerPrior > prev.CoderPrior+prev.ReviewerPrior {
			byOR[orSlug] = cand
		}
	}

	out := make([]protocol.CandidateModel, 0, len(byOR))
	for _, c := range byOR {
		out = append(out, c)
	}

	return out
}

func norm(idx *float64, maxVal float64) float64 {
	if idx == nil || maxVal <= 0 {
		return 0
	}

	n := *idx / maxVal
	if n < 0 {
		return 0
	}

	if n > 1 {
		return 1
	}

	return n
}

// maxIndices returns the response-wide maximum coding and intelligence indices,
// the normalization denominators shared by both catalog build legs.
func maxIndices(aa []aaModel) (maxCoding, maxIntel float64) {
	for _, m := range aa {
		if m.CodingIndex != nil && *m.CodingIndex > maxCoding {
			maxCoding = *m.CodingIndex
		}

		if m.IntelIndex != nil && *m.IntelIndex > maxIntel {
			maxIntel = *m.IntelIndex
		}
	}

	return maxCoding, maxIntel
}

// PriorOverride is an operator-supplied prior (already on the normalized 0..1
// scale) for an endpoint slug AA does not rate. Mapped from config in main.go.
type PriorOverride struct {
	Coder    float64
	Reviewer float64
}

// aaExclusionReason names why a served, tool-capable endpoint model did not
// become a selection candidate.
type aaExclusionReason string

const (
	exclUnmapped      aaExclusionReason = "no aa_model_map or model_priors entry"
	exclAASlugMissing aaExclusionReason = "mapped AA slug not found in the AA catalog"
	exclUnscored      aaExclusionReason = "mapped AA row has no usable scores"
	exclBelowFloor    aaExclusionReason = "below the quality floor for both roles"
)

// aaExclusion is one served, tool-capable endpoint model that produced no
// candidate. Siblings is populated only for exclUnscored: scored AA rows
// sharing the mapped slug's family-base prefix, with their normalized scores -
// a re-pointing hint for the operator. It is display-only and never feeds back
// into scoring.
type aaExclusion struct {
	Slug     string
	Reason   aaExclusionReason
	Siblings []aaSibling
}

// aaSibling is one scored AA variant row suggested as a re-pointing target.
type aaSibling struct {
	Slug     string
	Coder    float64
	Reviewer float64
}

// aaScored pairs a resolved candidate with the provenance of its priors for
// the refresh log: "model_priors override" or the exact AA slug it was scored
// from.
type aaScored struct {
	Candidate protocol.CandidateModel
	Source    string
}

// buildFromAAMap fuses Artificial Analysis priors with an endpoint catalog for
// the openai type. It iterates the endpoint's served models; for each it uses
// an operator override when present (AA join skipped for that slug), otherwise
// it joins the exact AA row named by aaModelMap: the single row whose Slug
// equals the mapped value, and only that row's coding/intelligence indices,
// normalized against the response-wide maxima. AA publishes separate rows per
// reasoning-effort variant with the base row frequently unscored, so a
// per-family aggregate would silently pin a gateway model to its strongest
// sibling variant's score; exact-row semantics keep the priors pointing at the
// variant the gateway actually serves. A nil index on the mapped row yields no
// prior for that role (the candidate competes only on the scored axis); with
// both axes nil the model produces no candidate and the exclusion carries the
// scored sibling variants as a re-pointing hint.
//
// A served, tool-capable model that is neither overridden nor mapped to a
// usable scored row is returned as an exclusion with its reason. Output
// candidates are keyed by endpoint slug and filtered to tool-capable,
// floor-clearing models.
func buildFromAAMap(aa []aaModel, endpoint map[string]orEntry, aaModelMap map[string]string, priors map[string]PriorOverride, floor float64) ([]aaScored, []aaExclusion) {
	maxCoding, maxIntel := maxIndices(aa)

	var (
		scored     []aaScored
		exclusions []aaExclusion
	)

	for slug, e := range endpoint {
		if !e.Tools {
			continue // endpoint reports the model cannot use tools
		}

		if p, ok := priors[slug]; ok {
			// Operator override: used verbatim, AA join skipped for this slug
			// (which leaves the creator unknown).
			if p.Coder < floor && p.Reviewer < floor {
				exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclBelowFloor})

				continue
			}

			scored = append(scored, aaScored{
				Candidate: protocol.CandidateModel{
					Slug:                  slug,
					PromptPricePerTok:     e.PromptPrice,
					CompletionPricePerTok: e.CompletionPrice,
					ContextWindow:         e.ContextWindow,
					CoderPrior:            p.Coder,
					ReviewerPrior:         p.Reviewer,
				},
				Source: "model_priors override",
			})

			continue
		}

		aaSlug, mapped := aaModelMap[slug]
		if !mapped {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclUnmapped})

			continue
		}

		row := slices.IndexFunc(aa, func(m aaModel) bool { return m.Slug == aaSlug })
		if row < 0 {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclAASlugMissing})

			continue
		}

		m := aa[row]

		if m.CodingIndex == nil && m.IntelIndex == nil {
			exclusions = append(exclusions, aaExclusion{
				Slug:     slug,
				Reason:   exclUnscored,
				Siblings: scoredSiblings(aa, aaSlug, maxCoding, maxIntel),
			})

			continue
		}

		coder := norm(m.CodingIndex, maxCoding)
		rev := norm(m.IntelIndex, maxIntel)

		if coder < floor && rev < floor {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclBelowFloor})

			continue
		}

		scored = append(scored, aaScored{
			Candidate: protocol.CandidateModel{
				Slug:                  slug,
				PromptPricePerTok:     e.PromptPrice,
				CompletionPricePerTok: e.CompletionPrice,
				ContextWindow:         e.ContextWindow,
				CoderPrior:            coder,
				ReviewerPrior:         rev,
				Creator:               m.Creator,
			},
			Source: aaSlug,
		})
	}

	return scored, exclusions
}

// scoredSiblings lists the scored AA rows sharing the mapped slug's family
// prefix, with their normalized scores. It first scans for "aaSlug-" prefixed
// variants (the usual case: the mapped slug is the unscored family base); if
// that yields nothing, it trims the last '-'-delimited segment from aaSlug and
// rescans, so a mapping that misses on an unscored variant slug still surfaces
// the scored base row and sibling branches. The result is display-only: it
// never feeds back into scoring.
func scoredSiblings(aa []aaModel, aaSlug string, maxCoding, maxIntel float64) []aaSibling {
	out := siblingsWithPrefix(aa, aaSlug, maxCoding, maxIntel)
	if len(out) > 0 {
		return out
	}

	base := aaSlug
	if i := strings.LastIndex(base, "-"); i > 0 {
		return siblingsWithPrefix(aa, base[:i], maxCoding, maxIntel)
	}

	return nil
}

// siblingsWithPrefix collects scored AA rows whose Slug has prefix+"-" as a
// prefix, excluding the mapped row itself and other unscored rows.
func siblingsWithPrefix(aa []aaModel, prefix string, maxCoding, maxIntel float64) []aaSibling {
	var out []aaSibling

	for _, m := range aa {
		if m.Slug == prefix || !strings.HasPrefix(m.Slug, prefix+"-") {
			continue
		}

		if m.CodingIndex == nil && m.IntelIndex == nil {
			continue
		}

		out = append(out, aaSibling{
			Slug:     m.Slug,
			Coder:    norm(m.CodingIndex, maxCoding),
			Reviewer: norm(m.IntelIndex, maxIntel),
		})
	}

	return out
}

// formatSiblings renders sibling suggestions as one pre-formatted
// "slug:coder=X,reviewer=Y" list for the exclusion WARN record, keeping the
// log record's attribute shape independent of sibling count.
func formatSiblings(sibs []aaSibling) string {
	parts := make([]string, 0, len(sibs))
	for _, s := range sibs {
		parts = append(parts, fmt.Sprintf("%s:coder=%.2f,reviewer=%.2f", s.Slug, s.Coder, s.Reviewer))
	}

	return strings.Join(parts, "; ")
}
