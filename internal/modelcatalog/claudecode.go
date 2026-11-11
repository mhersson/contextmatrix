package modelcatalog

import (
	"context"
	"slices"
	"strings"

	"github.com/mhersson/contextmatrix-protocol/selection"

	protocol "github.com/mhersson/contextmatrix-protocol"
)

// ClaudeCodeModel is one model the Claude Code subscription channel serves.
type ClaudeCodeModel struct {
	ID            string
	ContextWindow int
}

// ClaudeCodeModels is the static Claude Code catalog. Prices and priors are
// not listed here: they come from the Artificial Analysis fetch.
func ClaudeCodeModels() []ClaudeCodeModel {
	return []ClaudeCodeModel{
		{ID: "claude-opus-5-5", ContextWindow: 1_000_000},
		{ID: "claude-sonnet-5-5", ContextWindow: 1_000_000},
		{ID: "claude-haiku-5-5", ContextWindow: 1_000_000},
	}
}

// ClaudeCodeID maps a pin, a stored model name or a catalog ID to its Claude
// Code catalog ID. ok is false when the name maps to nothing in the catalog.
func ClaudeCodeID(name string) (string, bool) {
	id, ok := selection.ClaudeCodeModelID(name)
	if !ok {
		return "", false
	}

	if !slices.ContainsFunc(ClaudeCodeModels(), func(m ClaudeCodeModel) bool { return m.ID == id }) {
		return "", false
	}

	return id, true
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

// buildClaudeCode scores the static Claude Code catalog against the AA rows.
// It returns the candidates (Creator blanked) and the priced catalog.
func buildClaudeCode(aa []aaModel, floor float64) ([]protocol.CandidateModel, map[string]orEntry) {
	static := make(map[string]orEntry, len(ClaudeCodeModels()))
	for _, m := range ClaudeCodeModels() {
		static[m.ID] = orEntry{ContextWindow: m.ContextWindow, Tools: true}
	}

	scored, _, listPrices := buildEndpointCandidates(aa, static, nil, floor, nil, "")

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

	return cands, static
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
