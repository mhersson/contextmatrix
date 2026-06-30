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

	mu       sync.Mutex
	cached   []protocol.CandidateModel
	cachedAt time.Time
}

// NewBuilder constructs a Builder. floor<=0 defaults to 0.65; ttl<=0 to 6h.
func NewBuilder(aaKey string, floor float64, allowlist []string, ttl time.Duration) *Builder {
	if floor <= 0 {
		floor = 0.65
	}

	if ttl <= 0 {
		ttl = 6 * time.Hour
	}

	return &Builder{
		aaEndpoint: AADefaultEndpoint, orEndpoint: ORDefaultEndpoint,
		aaKey: aaKey, floor: floor, allowlist: allowlist, ttl: ttl,
	}
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

	if b.cached != nil && time.Since(b.cachedAt) < b.ttl {
		return b.cached
	}

	fresh, err := b.refresh(ctx)
	if err != nil {
		slog.Warn("model catalog refresh failed; using last-good", "error", err, "have", b.cached != nil)

		return b.cached
	}

	b.cached, b.cachedAt = fresh, time.Now()

	return fresh
}

func (b *Builder) refresh(ctx context.Context) ([]protocol.CandidateModel, error) {
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

	return build(aa, or, b.floor, b.allowlist), nil
}

// build is the pure transform: normalize indices against the response-wide
// max, keep trusted-creator models clearing the floor for at least one role,
// map to OR, collapse effort variants (same OR slug -> highest prior), join
// price/window/tools. Effort collapse falls out of keying by OR slug.
func build(aa []aaModel, or map[string]orEntry, floor float64, allow []string) []protocol.CandidateModel {
	var maxCoding, maxIntel float64
	for _, m := range aa {
		if m.CodingIndex != nil && *m.CodingIndex > maxCoding {
			maxCoding = *m.CodingIndex
		}

		if m.IntelIndex != nil && *m.IntelIndex > maxIntel {
			maxIntel = *m.IntelIndex
		}
	}

	if maxCoding <= 0 || maxIntel <= 0 {
		return nil
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
	var maxCoding, maxIntel float64
	for _, m := range aa {
		if m.CodingIndex != nil && *m.CodingIndex > maxCoding {
			maxCoding = *m.CodingIndex
		}

		if m.IntelIndex != nil && *m.IntelIndex > maxIntel {
			maxIntel = *m.IntelIndex
		}
	}

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
