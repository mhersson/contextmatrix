package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// EndpointModel is a picker-facing projection of a served, tool-capable model.
type EndpointModel struct {
	ID        string
	Label     string
	MaxTokens int
}

// FetchEndpointModels returns the endpoint's tool-capable models for the chat
// picker, reusing the same authenticated /models fetch as the catalog.
func FetchEndpointModels(ctx context.Context, baseURL, apiKey string) ([]EndpointModel, error) {
	cat, err := fetchEndpointCatalog(ctx, baseURL, apiKey)
	if err != nil {
		return nil, err
	}

	out := make([]EndpointModel, 0, len(cat))

	for id, e := range cat {
		if !e.Tools {
			continue
		}

		out = append(out, EndpointModel{ID: id, Label: id, MaxTokens: e.ContextWindow})
	}

	return out, nil
}

// EndpointModels projects the Builder's cached catalog to the picker's
// tool-capable model list, reusing the single cached /models fetch that Rate
// and Candidates already share instead of a second independent fetch. Nil
// receiver yields nil (same footgun guard as Candidates/Rate).
func (b *Builder) EndpointModels(ctx context.Context) []EndpointModel {
	if b == nil {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.refreshIfStaleLocked(ctx)

	out := make([]EndpointModel, 0, len(b.lastCatalog))

	for id, e := range b.lastCatalog {
		if !e.Tools {
			continue
		}

		out = append(out, EndpointModel{ID: id, Label: id, MaxTokens: e.ContextWindow})
	}

	return out
}

// endpointPricing is the pricing block of an OpenAI-compatible /models entry.
// Two dialects are in the wild and the key sets do not overlap, so both are
// decoded and whichever one the gateway populated is used:
//
//   - OpenRouter's: USD per *token*, as decimal strings.
//   - per-million: USD per 1M tokens, as JSON numbers.
//
// A gateway switching dialects (or dropping the block) silently zeroes every
// price, which is why endpointPricing.rate falls back across both and
// applyTokenCosts covers the remainder.
//
// Long-context step pricing (`tiers`) is deliberately not read: CM prices a
// model with one rate pair, and the base tier is the honest choice for the
// selector's price band. Cost accounting of a run that crosses a tier boundary
// therefore understates it - `token_costs` is the lever if that matters.
type endpointPricing struct {
	// OpenRouter dialect: USD per token, as strings.
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read"`
	InputCacheWrite string `json:"input_cache_write"`
	// Per-million dialect: USD per 1M tokens, as numbers.
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m"`
	CacheWritePer1M float64 `json:"cache_write_per_1m"`
}

// rate resolves the block to per-token USD. The per-token dialect wins when it
// prices anything at all; otherwise the per-million numbers are scaled down.
// Cache rates resolve independently of the prompt/completion pair, so a gateway
// that omits them still yields usable input/output rates (0 leaves the
// multiplier convention in PriceTokens to derive them).
func (p endpointPricing) rate() ModelPrice {
	perTok := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)

		return v
	}

	out := ModelPrice{
		Prompt:     perTok(p.Prompt),
		Completion: perTok(p.Completion),
		CacheRead:  perTok(p.InputCacheRead),
		CacheWrite: perTok(p.InputCacheWrite),
	}

	if out.Prompt == 0 && out.Completion == 0 {
		out.Prompt = p.InputPer1M / 1e6
		out.Completion = p.OutputPer1M / 1e6
	}

	if out.CacheRead == 0 {
		out.CacheRead = p.CacheReadPer1M / 1e6
	}

	if out.CacheWrite == 0 {
		out.CacheWrite = p.CacheWritePer1M / 1e6
	}

	return out
}

// fetchEndpointCatalog GETs an OpenAI-compatible /models listing (authenticated)
// and flattens it to the same orEntry shape used by the OpenRouter leg, so the
// fusion and cost code are leg-agnostic. Tool capability is read from
// capabilities.features, pricing from either dialect endpointPricing supports.
// A gateway that publishes no pricing at all yields zero-priced entries;
// alias_names is carried so applyTokenCosts can resolve those against the
// operator's rate table.
func fetchEndpointCatalog(ctx context.Context, endpoint, apiKey string) (map[string]orEntry, error) {
	url := strings.TrimRight(endpoint, "/") + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build endpoint request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("endpoint request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("endpoint unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		Data []struct {
			ID            string          `json:"id"`
			ContextLength int             `json:"context_length"`
			Pricing       endpointPricing `json:"pricing"`
			Capabilities  struct {
				Features []string `json:"features"`
			} `json:"capabilities"`
			AliasNames []string `json:"alias_names"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode endpoint response: %w", err)
	}

	out := make(map[string]orEntry, len(raw.Data))
	for _, d := range raw.Data {
		p := d.Pricing.rate()

		out[d.ID] = orEntry{
			PromptPrice:     p.Prompt,
			CompletionPrice: p.Completion,
			CacheReadPrice:  p.CacheRead,
			CacheWritePrice: p.CacheWrite,
			ContextWindow:   d.ContextLength,
			Tools:           slices.Contains(d.Capabilities.Features, "tools"),
			Aliases:         d.AliasNames,
		}
	}

	return out, nil
}
