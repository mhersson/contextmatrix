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
	"time"
)

// AADefaultEndpoint is the Artificial Analysis free-tier models endpoint
// (x-api-key auth, paginated at 200 models per page, 100 requests/day).
const AADefaultEndpoint = "https://artificialanalysis.ai/api/v2/language/models/free"

// aaMaxPages bounds the pagination loop: a runaway guard that also caps a
// full refresh at a tenth of the daily free-tier request quota. The catalog
// spans 3 pages today.
const aaMaxPages = 10

// aaFetchBudget bounds the whole pagination loop. Refreshes run under the
// Builder mutex, so without this a degraded AA server could stall every
// catalog consumer for aaMaxPages full request timeouts.
const aaFetchBudget = 60 * time.Second

// aaModel is the subset of an AA entry the selector needs.
type aaModel struct {
	Slug        string
	Creator     string
	CodingIndex *float64
	IntelIndex  *float64
}

type aaPage struct {
	Data []struct {
		Slug         string `json:"slug"`
		ModelCreator struct {
			Name string `json:"name"`
		} `json:"model_creator"`
		Evaluations struct {
			CodingIndex *float64 `json:"artificial_analysis_coding_index"`
			IntelIndex  *float64 `json:"artificial_analysis_intelligence_index"`
		} `json:"evaluations"`
	} `json:"data"`
	Pagination struct {
		HasMore bool `json:"has_more"`
	} `json:"pagination"`
}

// fetchAAModels pages through the AA catalog with the x-api-key header and
// flattens it, resolving each creator name to its OR vendor prefix.
func fetchAAModels(ctx context.Context, endpoint, key string) ([]aaModel, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse AA endpoint: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, aaFetchBudget)
	defer cancel()

	client := &http.Client{Timeout: 30 * time.Second}

	var out []aaModel

	for page := 1; ; page++ {
		raw, err := fetchAAPage(ctx, client, base, key, page)
		if err != nil {
			return nil, err
		}

		for _, d := range raw.Data {
			out = append(out, aaModel{
				Slug:        d.Slug,
				Creator:     creatorSlug(d.ModelCreator.Name),
				CodingIndex: d.Evaluations.CodingIndex,
				IntelIndex:  d.Evaluations.IntelIndex,
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
		return nil, errors.New("AA returned no models")
	}

	return out, nil
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
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return aaPage{}, fmt.Errorf("AA page %d: unexpected status %d", page, resp.StatusCode)
	}

	var raw aaPage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return aaPage{}, fmt.Errorf("decode AA page %d: %w", page, err)
	}

	return raw, nil
}
