package modelcatalog

import "testing"

func TestCreatorSlug(t *testing.T) {
	cases := map[string]string{
		// Table hits where the slugified name diverges from the OR prefix.
		"Alibaba": "qwen",
		"Kimi":    "moonshotai",
		// Slugify covers the rest of the trusted set.
		"OpenAI":    "openai",
		"Anthropic": "anthropic",
		"Google":    "google",
		"DeepSeek":  "deepseek",
		"MiniMax":   "minimax",
		"Z AI":      "z-ai",
		// Deliberately NOT mapped to x-ai: preserves the pre-migration
		// exclusion of xAI models from the trust gate.
		"SpaceXAI": "spacexai",
		// Unknown creators get a stable mechanical identity.
		"Frontier Labs Ltd.": "frontier-labs-ltd",
		"  Weird  Name!! ":   "weird-name",
		"":                   "",
	}

	for name, want := range cases {
		if got := creatorSlug(name); got != want {
			t.Errorf("creatorSlug(%q) = %q; want %q", name, got, want)
		}
	}
}

func TestTrustedCreatorParity(t *testing.T) {
	// The historically trusted AA display names must keep clearing the
	// default allowlist through the name-to-prefix resolution.
	for _, name := range []string{
		"OpenAI", "Anthropic", "Google", "DeepSeek",
		"Alibaba", "Z AI", "Kimi", "MiniMax",
	} {
		if !isTrusted(creatorSlug(name), nil) {
			t.Errorf("%q resolves to %q, which is not trusted", name, creatorSlug(name))
		}
	}

	if isTrusted(creatorSlug("SpaceXAI"), nil) {
		t.Error("SpaceXAI must stay outside the trust gate (see trustedCreators)")
	}
}

func TestMapAASlug(t *testing.T) {
	cases := map[string]string{
		"glm-5-2":           "z-ai/glm-5.2",
		"gpt-5-5":           "openai/gpt-5.5",
		"claude-opus-4-8":   "anthropic/claude-opus-4.8",
		"qwen3-7-max":       "qwen/qwen3.7-max",
		"deepseek-v4-flash": "deepseek/deepseek-v4-flash",
		"minimax-m3":        "minimax/minimax-m3",
	}

	for aa, creator := range map[string]string{
		"glm-5-2": "z-ai", "gpt-5-5": "openai", "claude-opus-4-8": "anthropic",
		"qwen3-7-max": "qwen", "deepseek-v4-flash": "deepseek", "minimax-m3": "minimax",
	} {
		got, ok := mapAASlug(aa, creator)
		if !ok || got != cases[aa] {
			t.Errorf("mapAASlug(%q,%q) = %q,%v; want %q", aa, creator, got, ok, cases[aa])
		}
	}
}

func TestMapAASlugEmptyCreatorRejected(t *testing.T) {
	if got, ok := mapAASlug("glm-5-2", ""); ok {
		t.Errorf("empty creator must not map, got %q", got)
	}
}

func TestMapAASlugOverrideWins(t *testing.T) {
	// The override table maps straight to a full OR slug and is consulted
	// before the creator prefix, so it works even with an empty creator.
	got, ok := mapAASlug("mistral-large-2512", "")
	if !ok || got != "mistralai/mistral-large-2512" {
		t.Errorf("override miss: %q,%v", got, ok)
	}
}
