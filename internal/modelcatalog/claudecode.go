package modelcatalog

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix-protocol/selection"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// ClaudeCodeID maps a pin, a stored model name or a catalog ID to its Claude
// Code catalog ID. ok is false when the name maps to nothing in the catalog.
func ClaudeCodeID(name string) (string, bool) {
	id, ok := selection.ClaudeCodeModelID(name)
	if !ok {
		return "", false
	}

	if !slices.ContainsFunc(selection.ClaudeCodeModels(), func(m selection.ClaudeCodeModel) bool { return m.ID == id }) {
		return "", false
	}

	return id, true
}

// claudeCodeContextWindow is the static window of a Claude Code catalog ID, 0
// for any other name. Only exact IDs match: a window lookup resolves no names.
func claudeCodeContextWindow(name string) int {
	for _, m := range selection.ClaudeCodeModels() {
		if m.ID == name {
			return m.ContextWindow
		}
	}

	return 0
}

// WithClaudeCode makes the Builder derive the Claude Code catalog from the
// Artificial Analysis fetch it already does. It needs the AA key.
func WithClaudeCode() BuilderOption {
	return func(b *Builder) { b.claudeCode = true }
}

// WithCatalogURLs overrides the Artificial Analysis and OpenRouter catalog
// URLs, for a mirror or a test server. An empty value keeps the default.
func WithCatalogURLs(aaURL, orURL string) BuilderOption {
	return func(b *Builder) {
		if aaURL != "" {
			b.aaEndpoint = aaURL
		}

		if orURL != "" {
			b.orEndpoint = orURL
		}
	}
}

// buildClaudeCode scores the protocol Claude Code catalog against the AA rows.
// It returns the candidates (Creator blanked), the priced catalog, each
// candidate's provenance and how many catalog IDs joined a scored AA row.
func buildClaudeCode(aa []aaModel, floor float64) ([]protocol.CandidateModel, map[string]orEntry, map[string]CandidateProvenance, int) {
	static := make(map[string]orEntry, len(selection.ClaudeCodeModels()))
	for _, m := range selection.ClaudeCodeModels() {
		static[m.ID] = orEntry{ContextWindow: m.ContextWindow, Tools: true}
	}

	scored, exclusions, listPrices := buildEndpointCandidates(aa, static, nil, floor, nil, "")

	cands := make([]protocol.CandidateModel, 0, len(scored))
	for _, s := range scored {
		c := s.Candidate
		c.Creator = ""
		cands = append(cands, c)
	}

	slices.SortFunc(cands, func(a, c protocol.CandidateModel) int {
		return strings.Compare(a.Slug, c.Slug)
	})

	applyAAListPrices(static, listPrices)

	return cands, static, scoredProvenance(scored), endpointServedScored(scored, exclusions)
}

// ClaudeCodeCandidates returns the Claude Code selection candidates, rated
// and priced from the shared AA fetch. Empty without an AA key or
// WithClaudeCode.
func (b *Builder) ClaudeCodeCandidates(ctx context.Context) []protocol.CandidateModel {
	if b == nil || !b.claudeCode {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return b.claudeCodeCached
}

// ClaudeCodeProvenance is Provenance for the Claude Code candidates, keyed by
// catalog ID. Nil without Claude Code data.
func (b *Builder) ClaudeCodeProvenance(ctx context.Context) map[string]CandidateProvenance {
	if b == nil || !b.claudeCode {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	return maps.Clone(b.claudeCodeProvenance)
}

// ClaudeCodeStats is Stats for the Claude Code candidates: the same AA
// snapshot, with ServedScored counting the catalog IDs that joined a scored
// AA row. Zero without Claude Code data.
func (b *Builder) ClaudeCodeStats(ctx context.Context) CatalogStats {
	if b == nil || !b.claudeCode {
		return CatalogStats{}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	if b.claudeCodeCached == nil {
		return CatalogStats{}
	}

	stats := b.stats
	stats.ServedScored = b.claudeCodeServedScored

	return stats
}

// ClaudeCodeRate returns the AA list price of a Claude Code catalog ID. ok is
// false for an unknown ID or a builder without Claude Code data.
func (b *Builder) ClaudeCodeRate(ctx context.Context, id string) (ModelPrice, bool) {
	if b == nil || !b.claudeCode {
		return ModelPrice{}, false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	e, ok := b.claudeCodeCatalog[id]
	if !ok || (e.PromptPrice == 0 && e.CompletionPrice == 0) {
		return ModelPrice{}, false
	}

	return ModelPrice{
		Prompt:     e.PromptPrice,
		Completion: e.CompletionPrice,
		CacheRead:  e.CacheReadPrice,
		CacheWrite: e.CacheWritePrice,
	}, true
}
