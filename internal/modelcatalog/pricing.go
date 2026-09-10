package modelcatalog

import (
	"sort"
	"strings"
)

// applyTokenCosts fills the per-token prices of endpoint models the gateway
// serves without a pricing block, reading them from the operator's token_costs
// rate table. An entry the gateway did price is left alone: the provider's own
// numbers win, token_costs only covers what the provider omits.
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
