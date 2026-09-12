package modelcatalog

import (
	"log/slog"
	"sort"
	"strings"
)

// priceSource names where a price came from, for the refresh log and the
// admin selector views. applyTokenCosts tags catalog entries with gateway or
// token_costs and applyAAListPrices with aa; candidatePrice (catalog.go)
// adds none for candidates.
type priceSource string

const (
	priceSourceGateway    priceSource = "gateway"
	priceSourceAA         priceSource = "aa"
	priceSourceTokenCosts priceSource = "token_costs"
	priceSourceNone       priceSource = "none"
)

// applyTokenCosts fills the per-token prices of endpoint models the gateway
// serves without a pricing block, reading them from the operator's token_costs
// rate table. An entry the gateway did price is left alone: the provider's
// own numbers win, token_costs only covers what the provider omits. It
// also tags each entry with where its price came from (`PriceSource`),
// which the candidate build reads to rank the gateway's price above the AA
// list price above the fill.
//
// Without this, a gateway that publishes no pricing hands the selector a
// catalog where every candidate costs 0. The price band then collapses to
// 0 * headroom = 0, admits every candidate, and the best-value rule degenerates
// to "highest prior wins" - the most expensive frontier model on every pick.
//
// Resolution per model, first hit wins: the served slug itself
// (anthropic/claude-opus-5), the slug with its vendor prefix stripped
// (claude-opus-5), then each gateway alias (claude-opus-5, global-opus-5, ...).
// The alias pass is what lets a rate table keyed on dated model names
// (claude-sonnet-4-5-20250929) price a gateway that serves the undated slug.
//
// It returns how many entries were priced and the tool-capable slugs still
// left without a price - the ones the selector will treat as free.
func applyTokenCosts(cat map[string]orEntry, costs map[string]ModelPrice) (filled int, unpriced []string) {
	for slug, e := range cat {
		if e.PromptPrice != 0 || e.CompletionPrice != 0 {
			e.PriceSource = priceSourceGateway
			cat[slug] = e

			continue
		}

		p, ok := lookupTokenCost(slug, e.Aliases, costs)
		if !ok {
			if e.Tools {
				unpriced = append(unpriced, slug)
			}

			continue
		}

		e.PromptPrice = p.Prompt
		e.CompletionPrice = p.Completion
		e.CacheReadPrice = p.CacheRead
		e.CacheWritePrice = p.CacheWrite
		e.PriceSource = priceSourceTokenCosts
		cat[slug] = e
		filled++
	}

	// Map iteration order is random; sort so refresh-to-refresh logs are
	// diffable (Served() sorts the same way).
	sort.Strings(unpriced)

	return filled, unpriced
}

// lookupTokenCost resolves one served model against the rate table by slug,
// then by vendor-stripped slug, then by gateway alias. A row that prices
// neither prompt nor completion tokens is treated as absent - it would leave
// the model free and is almost certainly a typo, not a genuinely free model.
func lookupTokenCost(slug string, aliases []string, costs map[string]ModelPrice) (ModelPrice, bool) {
	if len(costs) == 0 {
		return ModelPrice{}, false
	}

	keys := make([]string, 0, len(aliases)+2)
	keys = append(keys, slug)

	if _, name, found := strings.Cut(slug, "/"); found && name != "" {
		keys = append(keys, name)
	}

	keys = append(keys, aliases...)

	for _, k := range keys {
		if p, ok := costs[k]; ok && (p.Prompt != 0 || p.Completion != 0) {
			return p, true
		}
	}

	return ModelPrice{}, false
}

// applyAAListPrices writes the AA list price the join captured for each
// served slug onto its catalog entry, all four rates, tagging it aa, so
// Rate() prices card costs the way the selector priced the candidate. It
// replaces a token_costs fill: the list price is live, the table is whatever
// the operator last typed. A gateway-priced entry never appears in prices
// (candidatePrice ranks the gateway first), so it is never touched. Returns
// how many entries it priced.
func applyAAListPrices(cat map[string]orEntry, prices map[string]ModelPrice) (filled int) {
	for slug, p := range prices {
		e, ok := cat[slug]
		if !ok {
			continue
		}

		e.PromptPrice = p.Prompt
		e.CompletionPrice = p.Completion
		e.CacheReadPrice = p.CacheRead
		e.CacheWritePrice = p.CacheWrite
		e.PriceSource = priceSourceAA
		cat[slug] = e
		filled++
	}

	return filled
}

// warnUnpriced logs, once per refresh, each tool-capable served slug that no
// source priced: its card costs will report as 0. slugs is the list
// applyTokenCosts returned; entries priced since (by the AA fill) are skipped.
func warnUnpriced(cat map[string]orEntry, slugs []string) {
	for _, slug := range slugs {
		if e := cat[slug]; e.PromptPrice != 0 || e.CompletionPrice != 0 {
			continue
		}

		slog.Warn("endpoint model has no gateway, Artificial Analysis or token_costs price; card costs for it will report as 0",
			"slug", slug, "hint", "add a token_costs entry for this slug, its vendor-stripped name, or one of its gateway aliases")
	}
}
