package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractDescription_Nil(t *testing.T) {
	assert.Equal(t, "", ExtractDescription(nil))
}

func TestExtractDescription_PlainString(t *testing.T) {
	assert.Equal(t, "Fix the login page", ExtractDescription("Fix the login page"))
}

func TestExtractDescription_EmptyString(t *testing.T) {
	assert.Equal(t, "", ExtractDescription(""))
}

func TestExtractDescription_ADFSimple(t *testing.T) {
	// Minimal ADF: a doc with one paragraph containing text.
	adf := map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{
						"type": "text",
						"text": "Hello world",
					},
				},
			},
		},
	}

	result := ExtractDescription(adf)
	assert.Equal(t, "Hello world", result)
}

func TestExtractDescription_ADFMultipleParagraphs(t *testing.T) {
	adf := map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": "First paragraph"},
				},
			},
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": "Second paragraph"},
				},
			},
		},
	}

	result := ExtractDescription(adf)
	assert.Contains(t, result, "First paragraph")
	assert.Contains(t, result, "Second paragraph")
}

func TestExtractDescription_ADFWithHardBreak(t *testing.T) {
	adf := map[string]any{
		"type":    "doc",
		"version": 1,
		"content": []any{
			map[string]any{
				"type": "paragraph",
				"content": []any{
					map[string]any{"type": "text", "text": "Line one"},
					map[string]any{"type": "hardBreak"},
					map[string]any{"type": "text", "text": "Line two"},
				},
			},
		},
	}

	result := ExtractDescription(adf)
	assert.Contains(t, result, "Line one\nLine two")
}

func TestExtractDescription_UnexpectedType(t *testing.T) {
	// Falls back to fmt.Sprintf for unexpected types.
	result := ExtractDescription(42)
	assert.Equal(t, "42", result)
}
