package chat

// Pricer computes the estimated cost in USD for a token usage delta.
// The chat package declares the interface here so callers can wire any
// implementation (typically *service.CardService) without importing service
// directly, avoiding a circular dependency.
type Pricer interface {
	// PriceTokens returns the estimated USD cost for the given token counts
	// using the model's configured rates. Returns (cost, true) when the model
	// is known, (0, false) when it is not.
	PriceTokens(model string, prompt, cacheRead, cacheCreation, completion int64) (float64, bool)
}
