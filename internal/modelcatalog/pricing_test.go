package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTokenCostsResolutionOrder(t *testing.T) {
	costs := map[string]ModelPrice{
		"vendor/exact-slug":      {Prompt: 1e-6, Completion: 2e-6},
		"bare-name":              {Prompt: 3e-6, Completion: 4e-6, CacheRead: 5e-7, CacheWrite: 6e-6},
		"dated-alias-20250101":   {Prompt: 7e-6, Completion: 8e-6},
		"vendor/endpoint-priced": {Prompt: 9e-6, Completion: 9e-6},
		"free-row":               {},
	}

	cat := map[string]orEntry{
		"vendor/exact-slug": {Tools: true},
		"vendor/bare-name":  {Tools: true},
		"vendor/dated":      {Tools: true, Aliases: []string{"unknown-alias", "dated-alias-20250101"}},
		// The gateway priced this one: token_costs must not touch it.
		"vendor/endpoint-priced": {Tools: true, PromptPrice: 1e-9, CompletionPrice: 2e-9},
		// A rate row that prices nothing is treated as absent.
		"vendor/free-row": {Tools: true, Aliases: []string{"free-row"}},
		"vendor/unknown":  {Tools: true},
		// Not tool-capable: unpriced, but never reported (embeddings and the like
		// are not selection candidates).
		"vendor/embed": {},
	}

	filled, unpriced := applyTokenCosts(cat, costs)

	assert.Equal(t, 3, filled)
	assert.Equal(t, []string{"vendor/free-row", "vendor/unknown"}, unpriced)

	assert.InDelta(t, 1e-6, cat["vendor/exact-slug"].PromptPrice, 1e-15)
	assert.InDelta(t, 3e-6, cat["vendor/bare-name"].PromptPrice, 1e-15)
	assert.InDelta(t, 4e-6, cat["vendor/bare-name"].CompletionPrice, 1e-15)
	assert.InDelta(t, 5e-7, cat["vendor/bare-name"].CacheReadPrice, 1e-15)
	assert.InDelta(t, 6e-6, cat["vendor/bare-name"].CacheWritePrice, 1e-15)
	assert.InDelta(t, 7e-6, cat["vendor/dated"].PromptPrice, 1e-15)
	assert.InDelta(t, 1e-9, cat["vendor/endpoint-priced"].PromptPrice, 1e-15)
	assert.Zero(t, cat["vendor/unknown"].PromptPrice)

	assert.Equal(t, priceSourceGateway, cat["vendor/endpoint-priced"].PriceSource)
	assert.Equal(t, priceSourceTokenCosts, cat["vendor/exact-slug"].PriceSource)
	assert.Equal(t, priceSourceTokenCosts, cat["vendor/bare-name"].PriceSource)
	assert.Equal(t, priceSourceTokenCosts, cat["vendor/dated"].PriceSource)
	assert.Empty(t, cat["vendor/unknown"].PriceSource, "nothing priced it")
	assert.Empty(t, cat["vendor/free-row"].PriceSource, "a rate row pricing nothing is absent")
}

func TestApplyTokenCostsNoTable(t *testing.T) {
	cat := map[string]orEntry{"vendor/a": {Tools: true}}

	filled, unpriced := applyTokenCosts(cat, nil)

	assert.Zero(t, filled)
	assert.Equal(t, []string{"vendor/a"}, unpriced)
}

// TestBuilderPricesEndpointCandidatesFromTokenCosts is the regression guard for
// the whole point of the fallback: a gateway that publishes no pricing block
// must still hand the agent priced candidates, or the selector's price band
// collapses to zero and every pick goes to the highest prior.
func TestBuilderPricesEndpointCandidatesFromTokenCosts(t *testing.T) {
	endpointSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":200000,
			"alias_names":["model-a","eu-model-a"],
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer endpointSrv.Close()

	aaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"slug":"model-a","model_creator":{"name":"vendor"},
			"evaluations":{"artificial_analysis_coding_index":80,"artificial_analysis_intelligence_index":80}}]}`))
	}))
	defer aaSrv.Close()

	b := NewBuilder("aa-key", 0.5, []string{"vendor"}, time.Hour,
		WithEndpoint(endpointSrv.URL, "secret", nil),
		WithTokenCosts(map[string]ModelPrice{"model-a": {Prompt: 3e-6, Completion: 15e-6}}))
	b.aaEndpoint = aaSrv.URL

	cands := b.Candidates(context.Background())
	require.Len(t, cands, 1)
	assert.InDelta(t, 3e-6, cands[0].PromptPricePerTok, 1e-15)
	assert.InDelta(t, 15e-6, cands[0].CompletionPricePerTok, 1e-15)

	// The same fill feeds the cost paths through Rate.
	price, ok := b.Rate(context.Background(), "vendor/model-a")
	require.True(t, ok)
	assert.InDelta(t, 3e-6, price.Prompt, 1e-15)
}

func TestFetchEndpointCatalogCapturesAliases(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":1000,
			"alias_names":["model-a","eu-model-a"],"capabilities":{"features":["tools"]}}]}`))
	}))
	defer srv.Close()

	out, err := fetchEndpointCatalog(context.Background(), srv.URL, "secret")
	require.NoError(t, err)
	assert.Equal(t, []string{"model-a", "eu-model-a"}, out["vendor/model-a"].Aliases)
}
