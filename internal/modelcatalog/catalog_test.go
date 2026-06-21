package modelcatalog

import (
	"testing"
)

func TestBuildAppliesFloorAllowlistAndMapping(t *testing.T) {
	aa := []aaModel{
		{Slug: "glm-5-2", Creator: "zai", CodingIndex: f(76.5), IntelIndex: f(59.9)},   // max => norm 1.0
		{Slug: "weak-1", Creator: "openai", CodingIndex: f(30.0), IntelIndex: f(20.0)}, // norm .39 < floor .65
		{Slug: "untrusted-x", Creator: "longcat", CodingIndex: f(70), IntelIndex: f(50)},
	}
	or := map[string]orEntry{
		"z-ai/glm-5.2":  {PromptPrice: 1.2e-6, CompletionPrice: 4.1e-6, ContextWindow: 1048576, Tools: true},
		"openai/weak-1": {PromptPrice: 1e-7, CompletionPrice: 2e-7, ContextWindow: 8192, Tools: true},
	}

	got := build(aa, or, 0.65, nil)
	if len(got) != 1 {
		t.Fatalf("want 1 candidate (glm only), got %d: %+v", len(got), got)
	}

	c := got[0]
	if c.Slug != "z-ai/glm-5.2" || c.CoderPrior != 1.0 || c.ReviewerPrior != 1.0 || c.ContextWindow != 1048576 {
		t.Errorf("bad candidate: %+v", c)
	}
}

func TestBuildCollapsesEffortVariants(t *testing.T) {
	// Two AA slugs that map to the SAME OR slug (z-ai/glm-5.2); the
	// higher-prior variant must win the collapse. Weaker is listed first
	// so the replacement branch in build() is exercised.
	aa := []aaModel{
		{Slug: "glm-5.2", Creator: "zai", CodingIndex: f(50.0), IntelIndex: f(40.0)}, // weaker
		{Slug: "glm-5-2", Creator: "zai", CodingIndex: f(76.5), IntelIndex: f(59.9)}, // stronger (index max)
	}
	or := map[string]orEntry{
		"z-ai/glm-5.2": {PromptPrice: 1.2e-6, CompletionPrice: 4.1e-6, ContextWindow: 1048576, Tools: true},
	}

	got := build(aa, or, 0.65, nil)
	if len(got) != 1 {
		t.Fatalf("effort variants must collapse to 1 candidate, got %d: %+v", len(got), got)
	}

	if got[0].CoderPrior != 1.0 || got[0].ReviewerPrior != 1.0 {
		t.Errorf("collapse must keep the highest-prior variant, got %+v", got[0])
	}
}

func f(v float64) *float64 { return &v }
