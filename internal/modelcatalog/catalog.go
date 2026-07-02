package modelcatalog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// Builder fetches AA + OR on a TTL and produces the candidate set. Safe for
// concurrent use; serves the last-good snapshot when a refresh fails.
type Builder struct {
	aaEndpoint, orEndpoint, aaKey string
	floor                         float64
	allowlist                     []string
	ttl                           time.Duration

	// Endpoint leg (openai type). When endpointBaseURL != "", refresh() fuses
	// the endpoint catalog with AA priors via stemMap/priors instead of the OR leg.
	endpointBaseURL string
	endpointAPIKey  string
	stemMap         map[string]string
	priors          map[string]PriorOverride

	mu       sync.Mutex
	cached   []protocol.CandidateModel
	cachedAt time.Time
	// lastCatalog is the raw per-slug catalog from the most recent refresh (every
	// served model, not just selection candidates). Guarded by mu; consumed by
	// Rate() for per-slug cost lookups.
	lastCatalog map[string]orEntry
}

// BuilderOption configures a Builder after construction.
type BuilderOption func(*Builder)

// WithEndpoint switches the Builder to the openai endpoint leg: it fetches the
// endpoint's /v1/models (authenticated) and fuses with AA priors via stemMap,
// with per-slug operator overrides from priors.
func WithEndpoint(baseURL, apiKey string, stemMap map[string]string, priors map[string]PriorOverride) BuilderOption {
	return func(b *Builder) {
		b.endpointBaseURL = baseURL
		b.endpointAPIKey = apiKey
		b.stemMap = stemMap
		b.priors = priors
	}
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
// so. Must be called with b.mu held. On refresh failure it logs and leaves
// b.cached unchanged (last-good). On success it updates b.cached, b.cachedAt,
// and b.lastCatalog (via b.refresh).
func (b *Builder) refreshIfStaleLocked(ctx context.Context) {
	if b.cached != nil && time.Since(b.cachedAt) < b.ttl {
		return
	}

	fresh, err := b.refresh(ctx)
	if err != nil {
		slog.Warn("model catalog refresh failed; using last-good", "error", err, "have", b.cached != nil)

		return
	}

	b.cached, b.cachedAt = fresh, time.Now()
}

// Candidates returns the current candidate set, refreshing if the cache is
// stale. On refresh failure it logs and returns the last-good snapshot (nil
// only if no successful build has ever happened).
//
// A nil receiver yields nil (no candidates) without panicking — this handles
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

// Rate returns the per-token prices for slug from the most recent raw catalog
// (every served model, refreshing if stale). ok is false when the slug is not
// served. Unlike Candidates, this is not filtered to AA-rated/floor-clearing
// models, so picker-only and below-floor models are still priced.
func (b *Builder) Rate(ctx context.Context, slug string) (prompt, completion float64, ok bool) {
	if b == nil {
		return 0, 0, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	e, found := b.lastCatalog[slug]
	if !found {
		return 0, 0, false
	}

	return e.PromptPrice, e.CompletionPrice, true
}

func (b *Builder) refresh(ctx context.Context) ([]protocol.CandidateModel, error) {
	// Endpoint leg (openai type): pricing comes from the endpoint's own /models
	// and is independent of Artificial Analysis, so fetch it whenever configured
	// — even without an AA key. This lets a chat-only deployment (no agent
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

		cands := buildFromStemMap(aa, ep, b.stemMap, b.priors, b.floor)

		// "Served but unselectable" is a loud condition, not a silent one: a
		// tool-capable served model that yields no candidate (unmapped, no
		// override, or below the floor) means selection will fall back to the
		// default model for that quality. Surface it.
		toolCapable := 0

		for _, e := range ep {
			if e.Tools {
				toolCapable++
			}
		}

		if len(cands) < toolCapable {
			slog.Warn("endpoint models served but not selectable",
				"served_tool_capable", toolCapable, "candidates", len(cands),
				"reason", "unmapped to AA, no model_priors override, or below the quality floor")
		}

		return cands, nil
	}

	// OpenRouter leg still requires an AA key to normalize candidate indices.
	if b.aaKey == "" {
		return nil, fmt.Errorf("no AA API key configured")
	}

	aa, err := fetchAAModels(ctx, b.aaEndpoint, b.aaKey)
	if err != nil {
		return nil, err
	}

	or, err := fetchORCatalog(ctx, b.orEndpoint)
	if err != nil {
		return nil, err
	}

	b.lastCatalog = or

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

// buildFromStemMap fuses Artificial Analysis priors with an endpoint catalog for
// the openai type. It iterates the endpoint's served models; for each it uses an
// operator override when present (AA join skipped for that slug), otherwise it
// aggregates the strongest prior across the mapped AA stem and its variant rows.
//
// Family aggregation takes the best coder prior and the best reviewer prior
// INDEPENDENTLY across the stem's rows ("stem", "stem-*"). This deliberately
// differs from the OpenRouter build() collapse, which keeps both priors from the
// single highest combined-score row: AA populates variant rows inconsistently
// (the base slug is frequently unscored while a sibling variant carries the
// score), so a per-axis maximum is the safer family aggregation here. The two
// legs can therefore rank a shared model differently — accepted, documented.
//
// A served, tool-capable model that is neither overridden nor mapped is skipped
// (the caller counts these for the "served but unselectable" WARN). Output is
// keyed by endpoint slug and filtered to tool-capable, floor-clearing models.
func buildFromStemMap(aa []aaModel, endpoint map[string]orEntry, stemMap map[string]string, priors map[string]PriorOverride, floor float64) []protocol.CandidateModel {
	maxCoding, maxIntel := maxIndices(aa)

	out := make([]protocol.CandidateModel, 0, len(endpoint))

	for slug, e := range endpoint {
		if !e.Tools {
			continue // endpoint reports the model cannot use tools
		}

		var coder, rev float64

		if p, ok := priors[slug]; ok {
			// Operator override: used verbatim, AA join skipped for this slug.
			coder, rev = p.Coder, p.Reviewer
		} else {
			stem, mapped := stemMap[slug]
			if !mapped || maxCoding <= 0 || maxIntel <= 0 {
				continue // unmapped and no override, or AA has no usable scores
			}

			// Per-axis max across the stem family (see doc comment).
			for _, m := range aa {
				if m.Slug != stem && !strings.HasPrefix(m.Slug, stem+"-") {
					continue
				}

				if c := norm(m.CodingIndex, maxCoding); c > coder {
					coder = c
				}

				if r := norm(m.IntelIndex, maxIntel); r > rev {
					rev = r
				}
			}
		}

		if coder < floor && rev < floor {
			continue // below the quality floor for both roles
		}

		out = append(out, protocol.CandidateModel{
			Slug:                  slug,
			PromptPricePerTok:     e.PromptPrice,
			CompletionPricePerTok: e.CompletionPrice,
			ContextWindow:         e.ContextWindow,
			CoderPrior:            coder,
			ReviewerPrior:         rev,
		})
	}

	return out
}
