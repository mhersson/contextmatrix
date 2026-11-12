package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	"github.com/mhersson/contextmatrix/internal/llmchannel/llmchanneltest"
	"github.com/mhersson/contextmatrix/internal/modelcatalog"
)

// selectorChannel is a real holder switched to mode, with Claude Code serving
// the supported backends.
func selectorChannel(t *testing.T, mode llmchannel.Mode, supported []string) *llmchannel.Holder {
	t.Helper()

	holder, err := llmchannel.New(context.Background(), llmchannel.Config{
		Endpoint:     gatewayEndpoint(),
		OAuthToken:   "tok",
		Supported:    supported,
		DefaultModel: "claude-sonnet-5-5",
	}, llmchanneltest.Store{}, events.NewBus())
	require.NoError(t, err)
	require.NoError(t, holder.Set(context.Background(), mode, "human:test"))

	return holder
}

// claudeCodeSelectorCatalog is previewCatalog with a Claude Code catalog
// beside it. Blended prices per token: opus 3e-5, sonnet 1.8e-5, haiku 6e-6.
func claudeCodeSelectorCatalog() *stubSelectorCatalog {
	cat := previewCatalog()
	cat.claudeCandidates = []protocol.CandidateModel{
		{Slug: "claude-sonnet-5-5", CoderPrior: 0.84, ReviewerPrior: 0.84, PromptPricePerTok: 3e-6, CompletionPricePerTok: 1.5e-5, ContextWindow: 1_000_000},
		{Slug: "claude-opus-5-5", CoderPrior: 1, ReviewerPrior: 1, PromptPricePerTok: 5e-6, CompletionPricePerTok: 2.5e-5, ContextWindow: 1_000_000},
		{Slug: "claude-haiku-5-5", CoderPrior: 0.6, ReviewerPrior: 0.6, PromptPricePerTok: 1e-6, CompletionPricePerTok: 5e-6, ContextWindow: 1_000_000},
	}
	cat.claudeProvenance = map[string]modelcatalog.CandidateProvenance{
		"claude-opus-5-5":   {PriceSource: "aa", ScoredFrom: "claude-opus-5-5", PriorSource: modelcatalog.PriorSourceAA},
		"claude-sonnet-5-5": {PriceSource: "aa", ScoredFrom: "claude-sonnet-5-5", PriorSource: modelcatalog.PriorSourceAA},
		"claude-haiku-5-5":  {PriceSource: "aa", ScoredFrom: "claude-haiku-5-5", PriorSource: modelcatalog.PriorSourceAA},
	}
	cat.stats = modelcatalog.CatalogStats{IndexVersion: "5", AAScored: 94, ServedScored: 27, BestAAScore: 59.4}
	cat.claudeStats = modelcatalog.CatalogStats{IndexVersion: "5", AAScored: 94, ServedScored: 3, BestAAScore: 59.4}

	return cat
}

// channelSelectorHandlers carries favorites and a blacklist that each hold one
// Claude model slug and one slug without a Claude Code ID.
func channelSelectorHandlers(t *testing.T, mode llmchannel.Mode, supported []string) *selectorAdminHandlers {
	t.Helper()

	return &selectorAdminHandlers{
		store:     &stubSelectorAdminStore{},
		catalog:   claudeCodeSelectorCatalog(),
		blacklist: &stubBlacklist{slugs: []string{"anthropic/claude-opus-5.5", "c/weak"}},
		favorites: map[string]board.TierFavorites{
			"complex":  {ByRole: map[string][]string{"reviewer": {"anthropic/claude-sonnet-5.5", "b/pricey"}}},
			"critical": {ByRole: map[string][]string{"coder": {"a/cheap"}}},
		},
		reasoningEffort: "high",
		channel:         selectorChannel(t, mode, supported),
	}
}

func getSelectorCandidates(t *testing.T, h *selectorAdminHandlers) selectorCandidatesResponse {
	t.Helper()

	w := httptest.NewRecorder()
	h.getCandidates(w, httptest.NewRequest(http.MethodGet, "/api/admin/selector/candidates", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got selectorCandidatesResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	return got
}

func postSelectorPreview(t *testing.T, h *selectorAdminHandlers) selectorPreviewResponse {
	t.Helper()

	w := httptest.NewRecorder()
	h.preview(w, httptest.NewRequest(http.MethodPost, "/api/admin/selector/preview", jsonBody(t, defaultLadderBody())))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got selectorPreviewResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	return got
}

func candidateSlugs(views []selectorCandidateView) []string {
	out := make([]string, 0, len(views))
	for _, v := range views {
		out = append(out, v.Slug)
	}

	return out
}

func TestAdminSelectorCandidates_ClaudeCodeChannel(t *testing.T) {
	got := getSelectorCandidates(t, channelSelectorHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent}))

	assert.Equal(t, "claude_code", got.Catalog)
	assert.Equal(t, []string{"claude-haiku-5-5", "claude-opus-5-5", "claude-sonnet-5-5"}, candidateSlugs(got.Candidates))
	assert.Equal(t, "aa", got.Candidates[0].PriceSource)
	assert.Equal(t, "claude-haiku-5-5", got.Candidates[0].ScoredFrom)
	assert.Equal(t, []string{"claude-opus-5-5"}, got.Blacklist, "mapped; a slug without a Claude Code ID is dropped")
	assert.Equal(t, []protocol.FavoriteRule{{Tier: "complex", Role: "reviewer", Models: []string{"claude-sonnet-5-5"}}}, got.Favorites,
		"mapped; a rule left empty is dropped")
	assert.Equal(t, 3, got.ServedScoredModels)
	assert.Empty(t, got.ReasoningEffort, "the Claude Code catalog has no OpenAI models")
}

func TestAdminSelectorCandidates_EndpointChannel(t *testing.T) {
	tests := []struct {
		name      string
		mode      llmchannel.Mode
		supported []string
	}{
		{"endpoint channel", llmchannel.ModeEndpoint, []string{config.BackendNameAgent}},
		{"claude_code channel without the agent", llmchannel.ModeClaudeCode, []string{config.BackendNameChat}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSelectorCandidates(t, channelSelectorHandlers(t, tt.mode, tt.supported))

			assert.Equal(t, "endpoint", got.Catalog)
			assert.Equal(t, []string{"a/cheap", "a/mid", "b/pricey", "c/weak"}, candidateSlugs(got.Candidates))
			assert.Equal(t, []string{"anthropic/claude-opus-5.5", "c/weak"}, got.Blacklist)
			assert.Len(t, got.Favorites, 2)
			assert.Equal(t, 27, got.ServedScoredModels)
			assert.Equal(t, "high", got.ReasoningEffort)
		})
	}
}

func TestAdminSelectorCandidates_NoChannelIsEndpoint(t *testing.T) {
	h := channelSelectorHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent})
	h.channel = nil

	assert.Equal(t, "endpoint", getSelectorCandidates(t, h).Catalog)
}

func TestAdminSelectorPreview_ClaudeCodeChannel(t *testing.T) {
	got := postSelectorPreview(t, channelSelectorHandlers(t, llmchannel.ModeClaudeCode, []string{config.BackendNameAgent}))

	assert.Equal(t, "claude_code", got.Catalog)

	ids := map[string]bool{"claude-sonnet-5-5": true, "claude-haiku-5-5": true}

	for tier, p := range got.Tiers {
		for _, pick := range []selectorPickView{p.Coder.Pick, p.Reviewer.Pick} {
			assert.True(t, ids[pick.Model], "%s picked %q: only unblacklisted Claude Code IDs", tier, pick.Model)
		}

		for _, seat := range p.Panel {
			if seat.Pick.OK {
				assert.True(t, ids[seat.Pick.Model], "%s seated %q", tier, seat.Pick.Model)
			}
		}
	}

	complexTier := got.Tiers["complex"]
	assert.Equal(t, "claude-sonnet-5-5", complexTier.Reviewer.Pick.Model)
	assert.Equal(t, "favorite", complexTier.Reviewer.Pick.Source, "the mapped favorite applies")
	assert.Equal(t, "aa", complexTier.Reviewer.Pick.PriceSource)
}

func TestAdminSelectorPreview_EndpointChannel(t *testing.T) {
	got := postSelectorPreview(t, channelSelectorHandlers(t, llmchannel.ModeEndpoint, []string{config.BackendNameAgent}))

	assert.Equal(t, "endpoint", got.Catalog)
	assert.Equal(t, "a/cheap", got.Tiers["complex"].Coder.Pick.Model)
	assert.Equal(t, "b/pricey", got.Tiers["complex"].Reviewer.Pick.Model)
	assert.Equal(t, "favorite", got.Tiers["complex"].Reviewer.Pick.Source)
}
