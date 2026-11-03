package modelcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AADefaultEndpoint is the Artificial Analysis free-tier models endpoint
// (x-api-key auth, paginated at 200 models per page, 100 requests/day).
const AADefaultEndpoint = "https://artificialanalysis.ai/api/v2/language/models/free"

// aaMaxPages bounds the pagination loop: a runaway guard that also caps a
// full refresh at a tenth of the daily free-tier request quota. The catalog
// spans 4 pages today.
const aaMaxPages = 10

// aaFetchBudget bounds the whole pagination loop. Refreshes run under the
// Builder mutex, so without this a degraded AA server could stall every
// catalog consumer for aaMaxPages full request timeouts.
const aaFetchBudget = 60 * time.Second

// aaModel is the subset of an AA entry the selector needs. PromptPrice and
// CompletionPrice are AA's list price in USD per token, 0 when AA publishes
// none; priced is the test. CacheReadPrice and CacheWritePrice are the
// cache-hit and cache-write list prices, 0 when AA publishes none; they never
// decide priced and only reach card costs, never the selector.
type aaModel struct {
	Slug            string
	Creator         string
	IntelIndex      *float64
	PromptPrice     float64
	CompletionPrice float64
	CacheReadPrice  float64
	CacheWritePrice float64
}

// priced reports whether AA publishes a usable price for the row. A row that
// prices neither side counts as unpriced, the same rule token_costs uses for
// a rate row.
func (m aaModel) priced() bool {
	return m.PromptPrice != 0 || m.CompletionPrice != 0
}

// perMillionToPerToken scales an AA USD-per-million price to USD per token;
// nil (AA publishes none) is 0.
func perMillionToPerToken(perMillion *float64) float64 {
	if perMillion == nil {
		return 0
	}

	return *perMillion / 1e6
}

type aaPage struct {
	Data []struct {
		Slug         string `json:"slug"`
		ModelCreator struct {
			Name string `json:"name"`
		} `json:"model_creator"`
		Evaluations struct {
			IntelIndex *float64 `json:"artificial_analysis_intelligence_index"`
		} `json:"evaluations"`
		Pricing struct {
			Input      *float64 `json:"price_1m_input_tokens"`
			Output     *float64 `json:"price_1m_output_tokens"`
			CacheHit   *float64 `json:"price_1m_cache_hit_tokens"`
			CacheWrite *float64 `json:"price_1m_cache_write_tokens"`
		} `json:"pricing"`
	} `json:"data"`
	IndexVersion json.RawMessage `json:"intelligence_index_version"`
	Pagination   struct {
		HasMore bool `json:"has_more"`
	} `json:"pagination"`
}

// aaCatalog is the flattened AA catalog plus the page-level intelligence
// index version, "" when AA sent none.
type aaCatalog struct {
	Models       []aaModel
	IndexVersion string
}

// indexVersion reads the page-level version: a JSON number's literal text or
// a JSON string's trimmed value, else "". Any other shape is ignored.
func indexVersion(raw json.RawMessage) string {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}

	return ""
}

// fetchAAModels pages through the AA catalog with the x-api-key header and
// flattens it, resolving each creator name to its OR vendor prefix.
func fetchAAModels(ctx context.Context, endpoint, key string) (aaCatalog, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return aaCatalog{}, fmt.Errorf("parse AA endpoint: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, aaFetchBudget)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}

	var (
		out     []aaModel
		version string
	)

	for page := 1; ; page++ {
		raw, err := fetchAAPage(ctx, client, base, key, page)
		if err != nil {
			return aaCatalog{}, err
		}

		if version == "" {
			version = indexVersion(raw.IndexVersion)
		}

		for _, d := range raw.Data {
			out = append(out, aaModel{
				Slug:            d.Slug,
				Creator:         creatorSlug(d.ModelCreator.Name),
				IntelIndex:      d.Evaluations.IntelIndex,
				PromptPrice:     perMillionToPerToken(d.Pricing.Input),
				CompletionPrice: perMillionToPerToken(d.Pricing.Output),
				CacheReadPrice:  perMillionToPerToken(d.Pricing.CacheHit),
				CacheWritePrice: perMillionToPerToken(d.Pricing.CacheWrite),
			})
		}

		if !raw.Pagination.HasMore {
			break
		}

		if page == aaMaxPages {
			slog.Warn("AA pagination cap reached; catalog truncated",
				"pages", aaMaxPages, "models", len(out))

			break
		}
	}

	if len(out) == 0 {
		return aaCatalog{}, errors.New("AA returned no models")
	}

	return aaCatalog{Models: out, IndexVersion: version}, nil
}

func fetchAAPage(ctx context.Context, client *http.Client, base *url.URL, key string, page int) (aaPage, error) {
	u := *base
	q := u.Query()
	q.Set("page", strconv.Itoa(page))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return aaPage{}, fmt.Errorf("build AA request: %w", err)
	}

	req.Header.Set("x-api-key", key)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return aaPage{}, fmt.Errorf("AA page %d: %w", page, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return aaPage{}, fmt.Errorf("AA page %d: unexpected status %d", page, resp.StatusCode)
	}

	var raw aaPage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return aaPage{}, fmt.Errorf("decode AA page %d: %w", page, err)
	}

	return raw, nil
}
