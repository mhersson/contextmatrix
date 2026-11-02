package knowledge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeAnswer(t *testing.T) {
	item := map[string]any{"id": "R-1", "kind": "rule", "title": "t", "text": "x", "agreed": true, "x_extra": "kept"}
	text := func(s string) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
	}
	structured := func(v any) *mcp.CallToolResult { return &mcp.CallToolResult{StructuredContent: v} }

	tests := []struct {
		name      string
		res       *mcp.CallToolResult
		wantItems int
		wantErr   bool
		check     func(t *testing.T, a Answer)
	}{
		{
			"structured passes through, unknown fields kept", structured(map[string]any{"items": []any{item}, "note": "n", "x_top": 1}), 1, false,
			func(t *testing.T, a Answer) {
				assert.Equal(t, "kept", a["items"].([]any)[0].(map[string]any)["x_extra"])
				assert.Contains(t, a, "x_top")
				assert.Equal(t, json.Number("1"), a["x_top"])
				assert.Equal(t, "n", a["note"])
			},
		},
		{"text JSON used when no structured content", text(`{"items":[{"id":"R-1","kind":"rule","title":"t","text":"x"}]}`), 1, false, nil},
		{"items null becomes empty list", structured(map[string]any{"items": nil}), 0, false, nil},
		{"items absent becomes empty list", structured(map[string]any{}), 0, false, nil},
		{
			"null note and related dropped", structured(map[string]any{"items": []any{}, "note": nil, "related": nil, "missing": nil}), 0, false,
			func(t *testing.T, a Answer) {
				assert.NotContains(t, a, "note")
				assert.NotContains(t, a, "related")
				assert.NotContains(t, a, "missing")
			},
		},
		{
			"missing passes through", structured(map[string]any{"items": []any{}, "missing": []any{"R-9"}}), 0, false,
			func(t *testing.T, a Answer) { assert.Equal(t, []any{"R-9"}, a["missing"]) },
		},
		{"items not a list", structured(map[string]any{"items": "nope"}), 0, true, nil},
		{"item not an object", structured(map[string]any{"items": []any{"R-1"}}), 0, true, nil},
		{"note not a string", structured(map[string]any{"items": []any{}, "note": 3}), 0, true, nil},
		{"related not a list of objects", structured(map[string]any{"items": []any{}, "related": []any{"R-2"}}), 0, true, nil},
		{"missing not a list of strings", structured(map[string]any{"items": []any{}, "missing": []any{1}}), 0, true, nil},
		{"neither structured nor JSON text", text("not json"), 0, true, nil},
		{"no content at all", &mcp.CallToolResult{}, 0, true, nil},
		{"top level not an object", text(`[1,2]`), 0, true, nil},
		{"over the size cap", structured(map[string]any{"items": []any{map[string]any{"id": "R", "text": strings.Repeat("x", 2048)}}}), 0, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, raw, err := normalizeAnswer(tc.res, 1024)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrProviderFailed)

				if tc.name != "no content at all" {
					assert.NotEmpty(t, raw)
				}

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, raw)

			items, ok := a["items"].([]any)
			require.True(t, ok)
			require.NotNil(t, items)
			assert.Len(t, items, tc.wantItems)

			if tc.check != nil {
				tc.check(t, a)
			}
		})
	}
}
