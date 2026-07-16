package modelcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// AADefaultEndpoint is the Artificial Analysis models endpoint.
const AADefaultEndpoint = "https://artificialanalysis.ai/api/v2/data/llms/models"

// aaModel is the subset of an AA entry the selector needs.
type aaModel struct {
	Slug        string
	Creator     string
	CodingIndex *float64
	IntelIndex  *float64
}

type aaRaw struct {
	Data []struct {
		Slug         string `json:"slug"`
		ModelCreator struct {
			Slug string `json:"slug"`
		} `json:"model_creator"`
		Evaluations struct {
			CodingIndex *float64 `json:"artificial_analysis_coding_index"`
			IntelIndex  *float64 `json:"artificial_analysis_intelligence_index"`
		} `json:"evaluations"`
	} `json:"data"`
}

// fetchAAModels GETs the AA catalog with the x-api-key header and flattens it.
func fetchAAModels(ctx context.Context, endpoint, key string) ([]aaModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build AA request: %w", err)
	}

	req.Header.Set("x-api-key", key)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AA request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("AA unexpected status %d", resp.StatusCode)
	}

	var raw aaRaw
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode AA response: %w", err)
	}

	out := make([]aaModel, 0, len(raw.Data))
	for _, d := range raw.Data {
		out = append(out, aaModel{
			Slug:        d.Slug,
			Creator:     d.ModelCreator.Slug,
			CodingIndex: d.Evaluations.CodingIndex,
			IntelIndex:  d.Evaluations.IntelIndex,
		})
	}

	return out, nil
}
