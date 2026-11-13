package sentinel

import (
	"testing"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
)

// ratePricer prices its models per token kind: prompt, cache read, cache
// creation, completion.
type ratePricer map[string][4]float64

func (r ratePricer) PriceTokens(model string, prompt, cacheRead, cacheCreation, completion int64) (float64, bool) {
	rate, ok := r[model]
	if !ok {
		return 0, false
	}

	return float64(prompt)*rate[0] + float64(cacheRead)*rate[1] + float64(cacheCreation)*rate[2] + float64(completion)*rate[3], true
}

func TestPriceUsage(t *testing.T) {
	pricer := ratePricer{"vendor/model-a": {1e-5, 1e-6, 2e-5, 4e-5}}
	// 1000*1e-5 + 2000*1e-6 + 100*2e-5 + 150*4e-5 = 0.02
	tokens := protocol.LogTokenUsage{InputTokens: 1000, CacheReadTokens: 2000, CacheCreateTokens: 100, OutputTokens: 150}

	withCost := func(model string, usd float64) protocol.RunUsage {
		u := protocol.RunUsage{Model: model, LogTokenUsage: tokens}
		u.CostUSD = usd

		return u
	}

	tests := []struct {
		name string
		rows []protocol.RunUsage
		want float64
	}{
		{"no rows", nil, 0},
		{"gateway cost wins", []protocol.RunUsage{withCost("vendor/model-a", 0.15)}, 0.15},
		{"catalog estimate without a cost", []protocol.RunUsage{withCost("vendor/model-a", 0)}, 0.02},
		{"unpriced without a cost", []protocol.RunUsage{withCost("vendor/unknown", 0)}, 0},
		{"rows add up", []protocol.RunUsage{
			withCost("vendor/model-a", 0.15), withCost("vendor/model-a", 0), withCost("vendor/unknown", 0),
		}, 0.17},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.want, priceUsage(tt.rows, pricer), 1e-12)
		})
	}
}
