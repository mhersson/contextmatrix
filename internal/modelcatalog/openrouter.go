package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// ORDefaultEndpoint is the OpenRouter models catalog (no auth required).
const ORDefaultEndpoint = "https://openrouter.ai/api/v1/models"

// orEntry is the OR catalog data the selector needs, keyed by OR slug.
type orEntry struct {
	PromptPrice     float64
	CompletionPrice float64
	ContextWindow   int
	Tools           bool
}

func fetchORCatalog(ctx context.Context, endpoint string) (map[string]orEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build OR request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OR request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OR unexpected status %d", resp.StatusCode)
	}

	var raw struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode OR response: %w", err)
	}

	out := make(map[string]orEntry, len(raw.Data))
	for _, d := range raw.Data {
		pp, _ := strconv.ParseFloat(d.Pricing.Prompt, 64)
		cp, _ := strconv.ParseFloat(d.Pricing.Completion, 64)
		tools := false

		for _, p := range d.SupportedParameters {
			if p == "tools" {
				tools = true

				break
			}
		}

		out[d.ID] = orEntry{PromptPrice: pp, CompletionPrice: cp, ContextWindow: d.ContextLength, Tools: tools}
	}

	return out, nil
}
