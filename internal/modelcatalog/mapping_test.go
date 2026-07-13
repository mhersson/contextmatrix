package modelcatalog

import "testing"

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
		"glm-5-2": "zai", "gpt-5-5": "openai", "claude-opus-4-8": "anthropic",
		"qwen3-7-max": "alibaba", "deepseek-v4-flash": "deepseek", "minimax-m3": "minimax",
	} {
		got, ok := mapAASlug(aa, creator)
		if !ok || got != cases[aa] {
			t.Errorf("mapAASlug(%q,%q) = %q,%v; want %q", aa, creator, got, ok, cases[aa])
		}
	}
}

func TestMapAASlugOverrideWins(t *testing.T) {
	got, ok := mapAASlug("claude-opus-4-8", "anthropic")
	if !ok || got != "anthropic/claude-opus-4.8" {
		t.Errorf("override miss: %q,%v", got, ok)
	}
}
