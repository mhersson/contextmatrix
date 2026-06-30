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
