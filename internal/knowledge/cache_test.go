package knowledge

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
)

func TestItemKey_OrderInsensitiveAndRelatedSensitive(t *testing.T) {
	assert.Equal(t, itemKey("p", "s", []string{"b", "a"}, false), itemKey("p", "s", []string{"a", "b"}, false))
	assert.NotEqual(t, itemKey("p", "s", []string{"a"}, false), itemKey("p", "s", []string{"a"}, true))
	assert.NotEqual(t, itemKey("q", "s1", []string{"a"}, false), itemKey("q", "s2", []string{"a"}, false))
}

func TestItemKey_DoesNotMutateIDs(t *testing.T) {
	ids := []string{"b", "a"}
	itemKey("p", "s", ids, false)
	assert.Equal(t, []string{"b", "a"}, ids)
}

func TestItemCache(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := json.RawMessage(`{}`)

	t.Run("hit returns a copy", func(t *testing.T) {
		clk := clock.Fake(start)
		c := newItemCache(clk, 60*time.Second, 2)
		c.put("a", Answer{"items": []any{}}, raw)
		got, gotRaw, ok := c.get("a")
		require.True(t, ok)
		assert.Equal(t, raw, gotRaw)

		got["x"] = 1
		again, _, ok := c.get("a")
		require.True(t, ok)
		assert.NotContains(t, again, "x")
	})

	t.Run("expires after ttl", func(t *testing.T) {
		clk := clock.Fake(start)
		c := newItemCache(clk, 60*time.Second, 2)
		c.put("a", Answer{}, raw)
		clk.Advance(61 * time.Second)

		_, _, ok := c.get("a")
		assert.False(t, ok)
	})

	t.Run("evicts the earliest-expiring at capacity", func(t *testing.T) {
		clk := clock.Fake(start)
		c := newItemCache(clk, 60*time.Second, 2)
		c.put("a", Answer{}, raw)
		clk.Advance(time.Second)
		c.put("b", Answer{}, raw)
		clk.Advance(time.Second)
		c.put("c", Answer{}, raw)

		_, _, ok := c.get("a")
		assert.False(t, ok)
		_, _, ok = c.get("b")
		assert.True(t, ok)
		_, _, ok = c.get("c")
		assert.True(t, ok)
	})
}
