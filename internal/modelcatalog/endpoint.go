package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// fetchEndpointCatalog GETs an OpenAI-compatible /models listing (authenticated)
// and flattens it to the same orEntry shape used by the OpenRouter leg, so the
// fusion and cost code are leg-agnostic. Tool capability is read from
// capabilities.features.
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
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			Capabilities struct {
				Features []string `json:"features"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode endpoint response: %w", err)
	}

	out := make(map[string]orEntry, len(raw.Data))
	for _, d := range raw.Data {
		pp, _ := strconv.ParseFloat(d.Pricing.Prompt, 64)
		cp, _ := strconv.ParseFloat(d.Pricing.Completion, 64)

		tools := false

		for _, f := range d.Capabilities.Features {
			if f == "tools" {
				tools = true

				break
			}
		}

		out[d.ID] = orEntry{PromptPrice: pp, CompletionPrice: cp, ContextWindow: d.ContextLength, Tools: tools}
	}

	return out, nil
}
