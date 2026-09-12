package modelcatalog

import (
	"context"
	"log/slog"
	"maps"
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

// CandidateProvenance is where a candidate's inputs came from, for the admin
// selector views: PriceSource is gateway, aa, token_costs or none; ScoredFrom
// is the AA slug an automatic join scored the priors from, empty for a
// model_priors entry and on the OpenRouter leg.
type CandidateProvenance struct {
	PriceSource string
	ScoredFrom  string
}

// Builder fetches AA + OR on a TTL and produces the candidate set. Safe for
// concurrent use; serves the last-good snapshot when a refresh fails.
type Builder struct {
	aaEndpoint, orEndpoint, aaKey string
	floor                         float64
	allowlist                     []string
	ttl                           time.Duration

	// Endpoint leg (openai type). When endpointBaseURL != "", refresh() joins
	// the endpoint catalog to AA families automatically, with per-slug operator
	// overrides from priors, instead of the OR leg.
	endpointBaseURL string
	endpointAPIKey  string
	priors          map[string]PriorOverride
	// reasoningEffort is the effort the gateway pins for the OpenAI models
	// it serves (llm_endpoint.reasoning_effort); the join prefers the AA row
	// carrying it for OpenAI families and ignores it for every other
	// creator. Empty when unknown.
	reasoningEffort string
	// tokenCosts is the operator's token_costs rate table. On the endpoint leg
	// it prices models the gateway serves without a pricing block; see
	// applyTokenCosts.
	tokenCosts map[string]ModelPrice

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
	// names and snapshots resolve the model name a usage report carries to
	// a served id when it is not one itself: names holds the gateway's other
	// names for each model (vendor-stripped id, alias_names), snapshots the
	// snapshotKey of each served id and alias. Both are unique-match only.
	// Guarded by mu; rebuilt by setCatalog with lastCatalog.
	names     map[string]string
	snapshots map[string]string
	// provenance is where each candidate's price and priors came from, keyed
	// by slug, for the admin selector views. Guarded by mu; rebuilt with
	// cached on every successful refresh.
	provenance map[string]CandidateProvenance
	// lastRefreshAttempt is when refresh() was last invoked, success or
	// failure. Guarded by mu. Gates re-attempts on a stale cache so a failing
	// provider is retried at most once per refreshFailureCooldown.
	lastRefreshAttempt time.Time
}

// BuilderOption configures a Builder after construction.
type BuilderOption func(*Builder)

// WithEndpoint switches the Builder to the openai endpoint leg: it fetches the
// endpoint's /v1/models (authenticated), joins each served model to its AA
// family by canonical key, and applies per-slug operator overrides from
// priors.
func WithEndpoint(baseURL, apiKey string, priors map[string]PriorOverride) BuilderOption {
	return func(b *Builder) {
		b.endpointBaseURL = baseURL
		b.endpointAPIKey = apiKey
		b.priors = priors
	}
}

// WithReasoningEffort names the reasoning effort the openai endpoint pins for
// the OpenAI models it serves. A served OpenAI model whose id does not name
// its own effort is then scored from its family's row for this effort when
// AA has one; models of other creators are scored as if no effort were set.
func WithReasoningEffort(effort string) BuilderOption {
	return func(b *Builder) {
		b.reasoningEffort = effort
	}
}

// WithFavorites registers operator-configured favorite slugs; they pass the
// Served() vendor screen regardless of vendor.
func WithFavorites(favs []string) BuilderOption {
	return func(b *Builder) { b.favorites = favs }
}

// WithTokenCosts registers the operator's token_costs rate table. On the
// endpoint leg it prices models the gateway publishes without pricing, so the
// selector's price band has something to work with; the gateway's own prices
// still win where it publishes them.
func WithTokenCosts(costs map[string]ModelPrice) BuilderOption {
	return func(b *Builder) { b.tokenCosts = costs }
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

// LastRefreshed is when the snapshot Candidates serves was built, refreshing
// first if the cache is stale so it describes the snapshot a caller gets
// now. Zero until the first successful refresh, which is how the admin
// selector endpoints tell "no candidates" from "no catalog yet". A nil
// receiver is zero for the same reason Candidates yields nil.
func (b *Builder) LastRefreshed(ctx context.Context) time.Time {
	if b == nil {
		return time.Time{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return b.cachedAt
}

// Provenance reports where each candidate's price and priors came from,
// keyed by slug. It refreshes if stale, like Candidates, and describes the
// same snapshot. Nil on a nil receiver or before the first successful
// refresh.
func (b *Builder) Provenance(ctx context.Context) map[string]CandidateProvenance {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return maps.Clone(b.provenance)
}

// ModelPrice is the per-token price set for one served model. CacheRead and
// CacheWrite are zero when the gateway publishes no cache pricing; callers
// fall back to multiplier-derived rates.
type ModelPrice struct {
	Prompt, Completion, CacheRead, CacheWrite float64
}

// Rate returns the per-token price set for slug from the most recent raw
// catalog (every served model, refreshing if stale). ok is false when the
// slug is not served, or names two served models at once. Unlike
// Candidates, this is not filtered to
// AA-rated/floor-clearing models, so picker-only and below-floor models are
// still priced. slug may be the name the gateway echoed in a completion
// rather than the served id (claude-opus-5 for anthropic/claude-opus-5, or
// a dated snapshot); see entryFor.
func (b *Builder) Rate(ctx context.Context, slug string) (ModelPrice, bool) {
	if b == nil {
		return ModelPrice{}, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	e, found := b.entryFor(slug)
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

// entryFor resolves a model name to its catalog entry. A usage report
// carries the name the gateway echoed in the completion, which on an
// endpoint gateway that serves vendor-prefixed ids is the bare name
// (claude-opus-5) or a dated snapshot (gpt-5.4-2026-03-05), so the served
// id is tried first, then (endpoint leg only, see setCatalog) the gateway's
// other names for the model, then the served model that differs from the
// name only by a date token. Effort words are never
// stripped: sonar is not sonar-reasoning. A name two served models could
// claim resolves to neither. The first two tiers match the gateway's own
// spelling exactly; only the snapshot tier is case-insensitive. Caller
// holds b.mu.
func (b *Builder) entryFor(name string) (orEntry, bool) {
	if e, ok := b.lastCatalog[name]; ok {
		return e, true
	}

	if slug, ok := b.names[name]; ok {
		return b.lastCatalog[slug], true
	}

	if slug, ok := b.snapshots[snapshotKey(name)]; ok {
		return b.lastCatalog[slug], true
	}

	return orEntry{}, false
}

// setCatalog installs cat as the served catalog behind Rate, Served and
// Validate and rebuilds the name index entryFor reads. The index is built
// on the endpoint leg only: OpenRouter echoes the served slug in every
// completion, so a usage report there always hits the catalog directly and
// that leg keeps its exact-only lookup. Caller holds b.mu.
func (b *Builder) setCatalog(cat map[string]orEntry) {
	b.lastCatalog = cat
	b.names, b.snapshots = nil, nil

	if b.endpointBaseURL != "" {
		b.names, b.snapshots = indexCatalogNames(cat)
	}
}

// indexCatalogNames builds the two lookup maps entryFor falls back to,
// keyed on the names a gateway may echo for a served model: names by the
// vendor-stripped id and each alias, snapshots by the snapshotKey of the id
// and each alias. A name that is itself a served id is never indexed (the
// catalog is looked up directly), and a key two served models would claim
// is dropped from that map rather than guessed: an exact hit still prices
// those.
func indexCatalogNames(cat map[string]orEntry) (names, snapshots map[string]string) {
	names = make(map[string]string)
	snapshots = make(map[string]string)
	ambiguousName := map[string]bool{}
	ambiguousSnapshot := map[string]bool{}

	add := func(idx map[string]string, ambiguous map[string]bool, key, slug string) {
		if key == "" || ambiguous[key] {
			return
		}

		if prev, seen := idx[key]; seen && prev != slug {
			delete(idx, key)
			ambiguous[key] = true

			return
		}

		idx[key] = slug
	}

	addName := func(name, slug string) {
		if _, served := cat[name]; served {
			return
		}

		add(names, ambiguousName, name, slug)
	}

	for slug, e := range cat {
		if _, name, found := strings.Cut(slug, "/"); found && name != "" {
			addName(name, slug)
		}

		add(snapshots, ambiguousSnapshot, snapshotKey(slug), slug)

		for _, a := range e.Aliases {
			addName(a, slug)
			add(snapshots, ambiguousSnapshot, snapshotKey(a), slug)
		}
	}

	return names, snapshots
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

// Floor returns the quality floor configured for this Builder.
func (b *Builder) Floor() float64 {
	return b.floor
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

		// A gateway that publishes no pricing leaves every entry at 0, which
		// makes the selector's price band vacuous. Fill those from token_costs
		// before anything reads the catalog for card costs, and name what stays
		// unpriced. Whether the selector sees a price is decided per candidate
		// below, where the AA list price also counts.
		filled, unpriced := applyTokenCosts(ep, b.tokenCosts)
		if filled > 0 {
			slog.Info("endpoint models priced from token_costs", "count", filled)
		}

		// Without an AA key there are no selection candidates (the complexity
		// selector is an agent-only concern) and no AA list price to fall
		// back on, but per-slug pricing is populated.
		if b.aaKey == "" {
			warnUnpriced(ep, unpriced)

			b.setCatalog(ep)
			b.provenance = map[string]CandidateProvenance{}

			return []protocol.CandidateModel{}, nil
		}

		aa, err := fetchAAModels(ctx, b.aaEndpoint, b.aaKey)
		if err != nil {
			// The AA list price is part of the catalog's pricing now, so an
			// AA outage keeps the last-good priced catalog rather than
			// swapping in a fresh unpriced one. Only a first-ever refresh
			// adopts the served set so Rate, Served and Validate work.
			if b.lastCatalog == nil {
				b.setCatalog(ep)
			}

			return nil, err
		}

		built, exclusions, listPrices := buildEndpointCandidates(aa, ep, b.priors, b.floor, b.allowlist, b.reasoningEffort)

		// The join found an AA row for each served model it could; adopt its
		// list price for card costs where the gateway published none, so the
		// number the selector ranked on is the number the card is billed at,
		// and a pinned or chat-picked model outside the candidate set still
		// costs. Only what AA also cannot price is left for the operator.
		if n := applyAAListPrices(ep, listPrices); n > 0 {
			slog.Info("endpoint models priced from the Artificial Analysis list price; a token_costs fill for them is replaced", "count", n)
		}

		warnUnpriced(ep, unpriced)

		b.setCatalog(ep)

		// Deterministic audit trail: the build iterates the endpoint map, so
		// both lists arrive in map order. Sort by slug so refresh-to-refresh
		// logs are diffable (Served() sorts the same way).
		slices.SortFunc(exclusions, func(a, c aaExclusion) int { return strings.Compare(a.Slug, c.Slug) })
		slices.SortFunc(built, func(a, c aaScored) int { return strings.Compare(a.Candidate.Slug, c.Candidate.Slug) })

		// "Served but unselectable" is a loud condition, not a silent one: a
		// tool-capable served model that yields no candidate means selection
		// will fall back to the default model for that quality. One WARN per
		// excluded model, naming the slug, the reason, and what was tried.
		for _, x := range exclusions {
			attrs := []any{"slug", x.Slug, "reason", x.Reason}

			if len(x.Keys) > 0 {
				attrs = append(attrs, "keys", strings.Join(x.Keys, ","))
			}

			if len(x.Family) > 0 {
				attrs = append(attrs, "family", strings.Join(x.Family, ","))
			}

			if x.Source != "" {
				attrs = append(attrs, "source", x.Source)
			}

			slog.Warn("endpoint model not selectable", attrs...)
		}

		// Resolved candidate set: one line per served candidate with its
		// priors, how it was joined, the AA row it was scored from, and where
		// its price came from, so the operator can audit the join.
		for _, s := range built {
			slog.Info("endpoint model scored",
				"slug", s.Candidate.Slug, "coder_prior", s.Candidate.CoderPrior,
				"reviewer_prior", s.Candidate.ReviewerPrior, "join", s.Join,
				"source", s.Source, "effort", s.Effort, "price_source", s.PriceSource)

			if s.PriceSource == priceSourceNone {
				slog.Warn("candidate has no price from the gateway, Artificial Analysis or token_costs; the selector will treat it as free",
					"slug", s.Candidate.Slug)
			}
		}

		cands := make([]protocol.CandidateModel, 0, len(built))
		prov := make(map[string]CandidateProvenance, len(built))

		for _, s := range built {
			cands = append(cands, s.Candidate)

			p := CandidateProvenance{PriceSource: string(s.PriceSource)}
			if s.Join == joinAutomatic {
				p.ScoredFrom = s.Source
			}

			prov[s.Candidate.Slug] = p
		}

		b.provenance = prov

		return cands, nil
	}

	// OpenRouter leg: the OR catalog is public and unauthenticated - fetch it
	// even without an AA key so Rate/Served/Validate work on AA-less
	// deployments. Candidates still require AA to normalize prior indices.
	or, err := fetchORCatalog(ctx, b.orEndpoint)
	if err != nil {
		return nil, err
	}

	b.setCatalog(or)

	if b.aaKey == "" {
		b.provenance = map[string]CandidateProvenance{}

		return []protocol.CandidateModel{}, nil
	}

	aa, err := fetchAAModels(ctx, b.aaEndpoint, b.aaKey)
	if err != nil {
		return nil, err
	}

	cands := build(aa, or, b.floor, b.allowlist)

	// OpenRouter prices every model it serves: the served catalog is the
	// gateway for this leg. build does not report which AA row it scored a
	// slug from, so ScoredFrom stays empty here.
	prov := make(map[string]CandidateProvenance, len(cands))
	for _, c := range cands {
		prov[c.Slug] = CandidateProvenance{PriceSource: string(priceSourceGateway)}
	}

	b.provenance = prov

	return cands, nil
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
	exclNoFamily   aaExclusionReason = "no AA family matches this model"
	exclUnscored   aaExclusionReason = "AA family has no usable scores"
	exclNotAllowed aaExclusionReason = "creator not in the allowlist"
	exclBelowFloor aaExclusionReason = "below the quality floor for both roles"
)

// aaExclusion is one served, tool-capable endpoint model that produced no
// candidate, with what the join tried so the miss can be reported: Keys for
// exclNoFamily, Family for exclUnscored, Source (the AA slug it joined) when
// a join happened before the exclusion.
type aaExclusion struct {
	Slug   string
	Reason aaExclusionReason
	Keys   []string
	Family []string
	Source string
}

// candidatePrice resolves the price a selection candidate carries and where
// it came from: the gateway's own price when it published one, else the
// joined AA row's list price, else the token_costs fill, else 0. A priced
// entry not tagged token_costs is the gateway's, so an untagged catalog (a
// test fixture) still ranks correctly. applyAAListPrices then copies an AA
// price onto the catalog entry, so Rate() and card costs follow the same
// order. row is nil on the model_priors path.
func candidatePrice(e orEntry, row *aaModel) (prompt, completion float64, source priceSource) {
	entryPriced := e.PromptPrice != 0 || e.CompletionPrice != 0

	switch {
	case entryPriced && e.PriceSource != priceSourceTokenCosts:
		return e.PromptPrice, e.CompletionPrice, priceSourceGateway
	case row != nil && row.priced():
		return row.PromptPrice, row.CompletionPrice, priceSourceAA
	case entryPriced:
		return e.PromptPrice, e.CompletionPrice, priceSourceTokenCosts
	default:
		return 0, 0, priceSourceNone
	}
}

// How a candidate's priors were obtained, for the refresh log.
const (
	joinModelPriors = "model_priors"
	joinAutomatic   = "automatic"
)

// aaScored pairs a resolved candidate with the provenance of its priors for
// the refresh log - Join says how ("model_priors override" or the AA row it
// was scored from in Source), Effort the reasoning effort the join looked
// for (the served id's own suffix, else the gateway's for an OpenAI family;
// empty when neither) - and of its price.
type aaScored struct {
	Candidate   protocol.CandidateModel
	Join        string
	Source      string
	Effort      string
	PriceSource priceSource
}

// buildEndpointCandidates scores each tool-capable served slug for the
// openai leg. A model_priors override is used verbatim: no AA join, no
// allowlist screen, creator unknown. Every other slug joins its AA family
// automatically: the served id and each gateway alias reduce to family keys,
// the first key with rows wins, and the closest scored row in that family
// supplies the priors, preferring the row for the wanted reasoning effort
// (the served name's own suffix, else the gateway's `effort` argument, which
// reaches OpenAI families only); its creator must pass the allowlist.
// Everything that yields no floor-clearing candidate is returned as an
// exclusion with its reason and what was tried. listPrices carries, per
// served slug, the AA list price (all four rates) of the row the join chose
// whenever candidatePrice resolved the price to AA; it is captured before the
// allowlist and floor screens because those gate selection, not billing.
func buildEndpointCandidates(aa []aaModel, endpoint map[string]orEntry, priors map[string]PriorOverride, floor float64, allow []string, effort string) (scored []aaScored, exclusions []aaExclusion, listPrices map[string]ModelPrice) {
	maxCoding, maxIntel := maxIndices(aa)
	idx := indexFamilies(aa)

	listPrices = map[string]ModelPrice{}

	for slug, e := range endpoint {
		if !e.Tools {
			continue // endpoint reports the model cannot use tools
		}

		if p, ok := priors[slug]; ok {
			if p.Coder < floor && p.Reviewer < floor {
				exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclBelowFloor})

				continue
			}

			prompt, completion, priceSrc := candidatePrice(e, nil)

			scored = append(scored, aaScored{
				Candidate: protocol.CandidateModel{
					Slug:                  slug,
					PromptPricePerTok:     prompt,
					CompletionPricePerTok: completion,
					ContextWindow:         e.ContextWindow,
					CoderPrior:            p.Coder,
					ReviewerPrior:         p.Reviewer,
				},
				Join:        joinModelPriors,
				Source:      "model_priors override",
				PriceSource: priceSrc,
			})

			continue
		}

		keys := lookupKeys(slug, e.Aliases)

		lk, found := firstFamily(idx, keys)
		if !found {
			tried := make([]string, 0, len(keys))
			for _, k := range keys {
				tried = append(tried, k.key)
			}

			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclNoFamily, Keys: tried})

			continue
		}

		want := lk.effort
		if want == "" && idx.creator(lk.key) == creatorOpenAI {
			want = effort
		}

		m, ok := idx.closest(lk.key, want, maxCoding, maxIntel)
		if !ok {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclUnscored, Family: idx.slugs(lk.key)})

			continue
		}

		prompt, completion, priceSrc := candidatePrice(e, &m)
		if priceSrc == priceSourceAA {
			listPrices[slug] = ModelPrice{
				Prompt:     m.PromptPrice,
				Completion: m.CompletionPrice,
				CacheRead:  m.CacheReadPrice,
				CacheWrite: m.CacheWritePrice,
			}
		}

		if !isTrusted(m.Creator, allow) {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclNotAllowed, Source: m.Slug})

			continue
		}

		coder := norm(m.CodingIndex, maxCoding)
		rev := norm(m.IntelIndex, maxIntel)

		if coder < floor && rev < floor {
			exclusions = append(exclusions, aaExclusion{Slug: slug, Reason: exclBelowFloor, Source: m.Slug})

			continue
		}

		scored = append(scored, aaScored{
			Candidate: protocol.CandidateModel{
				Slug:                  slug,
				PromptPricePerTok:     prompt,
				CompletionPricePerTok: completion,
				ContextWindow:         e.ContextWindow,
				CoderPrior:            coder,
				ReviewerPrior:         rev,
				Creator:               m.Creator,
			},
			Join:        joinAutomatic,
			Source:      m.Slug,
			Effort:      want,
			PriceSource: priceSrc,
		})
	}

	return scored, exclusions, listPrices
}

// lookupKey is one family key a served model is looked up under, with the
// reasoning effort the name itself carried (gpt-5.2-high names high), empty
// when it carried none.
type lookupKey struct {
	key    string
	effort string
}

// lookupKeys is the ordered family keys a served model is looked up under:
// its id (familyKey drops any vendor prefix, so the vendor-stripped id is
// the same key), then each gateway alias, without duplicate keys or empties.
func lookupKeys(slug string, aliases []string) []lookupKey {
	names := make([]string, 0, len(aliases)+1)
	names = append(names, slug)
	names = append(names, aliases...)

	keys := make([]lookupKey, 0, len(names))
	seen := map[string]bool{}

	for _, n := range names {
		k, efforts, _ := familyKeyParts(n)
		if k == "" || seen[k] {
			continue
		}

		seen[k] = true

		lk := lookupKey{key: k}
		if len(efforts) > 0 {
			lk.effort = efforts[0]
		}

		keys = append(keys, lk)
	}

	return keys
}

// firstFamily returns the first key that has AA rows.
func firstFamily(idx familyIndex, keys []lookupKey) (lookupKey, bool) {
	for _, k := range keys {
		if len(idx[k.key]) > 0 {
			return k, true
		}
	}

	return lookupKey{}, false
}
