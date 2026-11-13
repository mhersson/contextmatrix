package sentinel

import protocol "github.com/mhersson/contextmatrix-protocol"

// Pricer estimates a model's cost from CM's rate table and catalog; false when
// the model has no price.
type Pricer interface {
	PriceTokens(model string, prompt, cacheRead, cacheCreation, completion int64) (float64, bool)
}

// priceUsage charges each row its gateway cost, else the catalog estimate. A
// row priced neither way charges nothing.
func priceUsage(rows []protocol.RunUsage, p Pricer) float64 {
	var usd float64

	for _, r := range rows {
		if r.CostUSD > 0 {
			usd += r.CostUSD

			continue
		}

		if est, ok := p.PriceTokens(r.Model, r.InputTokens, r.CacheReadTokens, r.CacheCreateTokens, r.OutputTokens); ok {
			usd += est
		}
	}

	return usd
}

// usageWindows collects the subscription windows the usage rows carry.
func usageWindows(rows []protocol.RunUsage) []protocol.SubscriptionUsage {
	var windows []protocol.SubscriptionUsage

	for _, r := range rows {
		windows = append(windows, r.Subscription...)
	}

	return windows
}
