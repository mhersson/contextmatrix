package modelcatalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchEndpointCatalog(t *testing.T) {
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","context_length":200000,
			 "pricing":{"prompt":"0.000003","completion":"0.000015","input_cache_read":"0.0000005","input_cache_write":"0.00000625"},
			 "capabilities":{"features":["streaming","tools"]}},
			{"id":"model-b","context_length":128000,
			 "pricing":{"prompt":"0.0000007","completion":"0.000003"},
			 "capabilities":{"features":["streaming"]}}
		]}`))
	}))
	defer srv.Close()

	out, err := fetchEndpointCatalog(context.Background(), srv.URL, "secret")
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret", gotAuth)
	require.Contains(t, out, "model-a")
	assert.True(t, out["model-a"].Tools)
	assert.Equal(t, 200000, out["model-a"].ContextWindow)
	assert.InDelta(t, 0.000003, out["model-a"].PromptPrice, 1e-12)
	assert.InDelta(t, 0.000015, out["model-a"].CompletionPrice, 1e-12)
	assert.InDelta(t, 0.0000005, out["model-a"].CacheReadPrice, 1e-12)
	assert.InDelta(t, 0.00000625, out["model-a"].CacheWritePrice, 1e-12)
	require.Contains(t, out, "model-b")
	assert.False(t, out["model-b"].Tools)
}

func TestFetchEndpointCatalog_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := fetchEndpointCatalog(context.Background(), srv.URL, "bad-key")
	assert.Error(t, err)
}

// TestFetchEndpointCatalogPerMillionPricing covers the second pricing dialect:
// USD per 1M tokens as JSON numbers. A gateway switching to it (as one did,
// mid-life, without notice) silently zeroed every price and left the selector's
// price band vacuous.
func TestFetchEndpointCatalogPerMillionPricing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"vendor/model-a","context_length":1000000,
			 "pricing":{"input_per_1m":5.5,"output_per_1m":27.5,"cache_read_per_1m":0.55,"cache_write_per_1m":6.875},
			 "capabilities":{"features":["tools"]}},
			{"id":"vendor/no-cache-rates","context_length":1050000,
			 "pricing":{"input_per_1m":2.5,"output_per_1m":15,"cache_read_per_1m":0.25,
			            "tiers":[{"input_per_1m":10,"output_per_1m":45,"from_tokens":272000}]},
			 "capabilities":{"features":["tools"]}}
		]}`))
	}))
	defer srv.Close()

	out, err := fetchEndpointCatalog(context.Background(), srv.URL, "secret")
	require.NoError(t, err)

	a := out["vendor/model-a"]
	assert.InDelta(t, 5.5e-6, a.PromptPrice, 1e-15)
	assert.InDelta(t, 27.5e-6, a.CompletionPrice, 1e-15)
	assert.InDelta(t, 0.55e-6, a.CacheReadPrice, 1e-15)
	assert.InDelta(t, 6.875e-6, a.CacheWritePrice, 1e-15)

	// Tier rates are ignored: the base pair prices the model.
	b := out["vendor/no-cache-rates"]
	assert.InDelta(t, 2.5e-6, b.PromptPrice, 1e-15)
	assert.InDelta(t, 15e-6, b.CompletionPrice, 1e-15)
	assert.Zero(t, b.CacheWritePrice)
}

// TestFetchEndpointCatalogPrefersPerTokenDialect proves the per-token strings
// win when a response carries both dialects.
func TestFetchEndpointCatalogPrefersPerTokenDialect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"vendor/model-a","context_length":1000,
			"pricing":{"prompt":"0.000003","completion":"0.000015","input_per_1m":99,"output_per_1m":99},
			"capabilities":{"features":["tools"]}}]}`))
	}))
	defer srv.Close()

	out, err := fetchEndpointCatalog(context.Background(), srv.URL, "secret")
	require.NoError(t, err)
	assert.InDelta(t, 3e-6, out["vendor/model-a"].PromptPrice, 1e-15)
	assert.InDelta(t, 15e-6, out["vendor/model-a"].CompletionPrice, 1e-15)
}
