package modelcatalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
		"SpaceXAI":  "x-ai",
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
		"Z AI", "Kimi", "MiniMax", "SpaceXAI",
	} {
		if !isTrusted(creatorSlug(name), nil) {
			t.Errorf("%q resolves to %q, which is not trusted", name, creatorSlug(name))
		}
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

// TestFamilyKey is the regression table for the join key: the spec's
// examples, every date-token shape, every effort suffix, and the shapes that
// must never be stripped.
func TestFamilyKey(t *testing.T) {
	cases := map[string]string{
		// spec examples
		"anthropic/claude-opus-5":                         "claude-opus-5",
		"gpt-5.2":                                         "gpt-5-2",
		"claude-sonnet-4-5-20250929":                      "claude-sonnet-4-5",
		"deepseek-v4-flash-0420-high":                     "deepseek-v4-flash",
		"gemini-2-5-flash-lite-preview-09-2025-reasoning": "gemini-2-5-flash-lite-preview",
		"gpt-5-2-codex":                                   "gpt-5-2-codex",
		"gpt-5-4-mini-non-reasoning":                      "gpt-5-4-mini",
		// case and vendor prefix
		"OpenAI/GPT-5.2": "gpt-5-2",
		// date shapes
		"gpt-4o-2024-08-06":        "gpt-4o",
		"claude-35-sonnet-june-24": "claude-35-sonnet",
		"gemini-1-5-pro-may-2024":  "gemini-1-5-pro",
		"gpt-5-5-instant-05-26":    "gpt-5-5-instant",
		"kimi-k2-0905":             "kimi-k2",
		"gemini-2-5-flash-04-2025": "gemini-2-5-flash",
		// effort shapes
		"gpt-5-minimal":                "gpt-5",
		"claude-opus-4-6-adaptive":     "claude-opus-4-6",
		"claude-4-sonnet-thinking":     "claude-4-sonnet",
		"grok-4-20-0309-non-reasoning": "grok-4-20",
		"gpt-5-6-luna-xhigh":           "gpt-5-6-luna",
		// never stripped: single-digit versions, sizes, short numbers, identity words
		"grok-4-20":                 "grok-4-20",
		"gemini-2-0-flash-lite-001": "gemini-2-0-flash-lite-001",
		"minimax-m1-40k":            "minimax-m1-40k",
		"gemma-3-270m":              "gemma-3-270m",
		"qwen-2-5-72b":              "qwen-2-5-72b",
		"gemini-3-1-pro-preview":    "gemini-3-1-pro-preview",
		"deepseek-v4-flash-vision":  "deepseek-v4-flash-vision",
		// degenerate
		"high": "high",
		"":     "",
	}

	for in, want := range cases {
		assert.Equal(t, want, familyKey(in), in)
	}
}

func TestFamilyKeyCounts(t *testing.T) {
	key, effort, date := familyKeyCounts("deepseek-v4-flash-0420-high")
	assert.Equal(t, "deepseek-v4-flash", key)
	assert.Equal(t, 1, effort)
	assert.Equal(t, 1, date)

	key, effort, date = familyKeyCounts("gpt-5-2")
	assert.Equal(t, "gpt-5-2", key)
	assert.Zero(t, effort)
	assert.Zero(t, date)
}

func TestRewriteKeys(t *testing.T) {
	assert.Equal(t, []string{"claude-sonnet-4-5"}, rewriteKeys("claude-4-5-sonnet"))
	assert.Equal(t, []string{"claude-opus-4-1"}, rewriteKeys("claude-4-1-opus"))
	assert.Nil(t, rewriteKeys("claude-sonnet-4-5"), "already name-first")
	assert.Nil(t, rewriteKeys("claude-2"), "no name segment")
	assert.Nil(t, rewriteKeys("gpt-4-5-turbo"), "not an Anthropic key")
}

// TestIndexFamiliesClosest pins the row choice: the scored base row over a
// stronger variant, a dated base over a dated variant, the strongest
// equally-close variant when the base is unscored, nothing when no row is
// scored, and reachability through the rewrite rule and the override table.
func TestIndexFamiliesClosest(t *testing.T) {
	aa := []aaModel{
		{Slug: "gpt-5-2", Creator: "openai", CodingIndex: new(60.0), IntelIndex: new(60.0)},
		{Slug: "gpt-5-2-medium", Creator: "openai", CodingIndex: new(80.0), IntelIndex: new(80.0)},
		{Slug: "deepseek-v4-flash-0420", Creator: "deepseek", CodingIndex: new(50.0), IntelIndex: new(50.0)},
		{Slug: "deepseek-v4-flash-0420-high", Creator: "deepseek", CodingIndex: new(70.0), IntelIndex: new(70.0)},
		{Slug: "kimi-k3", Creator: "moonshotai"},
		{Slug: "kimi-k3-low", Creator: "moonshotai", CodingIndex: new(40.0), IntelIndex: new(40.0)},
		{Slug: "kimi-k3-high", Creator: "moonshotai", CodingIndex: new(75.0), IntelIndex: new(75.0)},
		{Slug: "ghost-1", Creator: "x"},
		{Slug: "claude-4-5-sonnet", Creator: "anthropic", CodingIndex: new(65.0), IntelIndex: new(65.0)},
		{Slug: "claude-35-sonnet", Creator: "anthropic", CodingIndex: new(30.0), IntelIndex: new(30.0)},
	}
	idx := indexFamilies(aa)

	pick := func(key string) string {
		m, ok := idx.closest(key, 80, 80)
		if !ok {
			return ""
		}

		return m.Slug
	}

	assert.Equal(t, "gpt-5-2", pick("gpt-5-2"), "the scored base row wins over a stronger effort variant")
	assert.Equal(t, "deepseek-v4-flash-0420", pick("deepseek-v4-flash"), "a dated base beats a dated effort variant")
	assert.Equal(t, "kimi-k3-high", pick("kimi-k3"), "unscored base: the strongest equally-close variant")
	assert.Empty(t, pick("ghost-1"), "no scored row")
	assert.Empty(t, pick("nope"), "no family")
	assert.Equal(t, "claude-4-5-sonnet", pick("claude-sonnet-4-5"), "reachable through the Anthropic rewrite")
	assert.Equal(t, "claude-35-sonnet", pick("claude-3-5-sonnet"), "reachable through the override table")

	assert.Equal(t, []string{"ghost-1"}, idx.slugs("ghost-1"))
	assert.Equal(t, []string{"kimi-k3", "kimi-k3-high", "kimi-k3-low"}, idx.slugs("kimi-k3"))
	assert.Empty(t, idx.slugs("nope"))
}
