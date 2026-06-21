package modelcatalog

import (
	"context"
	"fmt"
	"log/slog"
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
func (b *Builder) Candidates(ctx context.Context) []protocol.CandidateModel {
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

func norm(idx *float64, max float64) float64 {
	if idx == nil || max <= 0 {
		return 0
	}
	n := *idx / max
	if n < 0 {
		return 0
	}
	if n > 1 {
		return 1
	}
	return n
}
