package sentinel

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func TestPackSection(t *testing.T) {
	const kib = 1 << 10

	tests := []struct {
		name              string
		proposed, pending []state.Finding
		max               int
		section           string
		ids               []int64
	}{
		{
			"proposed first, then pending in order",
			[]state.Finding{guidance(1, "P1")},
			[]state.Finding{guidance(2, "G1"), guidance(3, "G2")},
			16 * kib, "P1\n\nG1\n\nG2",
			[]int64{1, 2, 3},
		},
		{
			"a finding that does not fit is skipped and the next one tried",
			[]state.Finding{guidance(1, strings.Repeat("p", 2*kib))},
			[]state.Finding{guidance(2, strings.Repeat("g", 15*kib)), guidance(3, "G2")},
			16 * kib, strings.Repeat("p", 2*kib) + "\n\nG2",
			[]int64{1, 3},
		},
		{
			"the separators count toward the cap",
			nil,
			[]state.Finding{guidance(1, "aaaa"), guidance(2, "bb")},
			8, "aaaa\n\nbb",
			[]int64{1, 2},
		},
		{
			"one byte over the cap",
			nil,
			[]state.Finding{guidance(1, "aaaa"), guidance(2, "bbb")},
			8, "aaaa",
			[]int64{1},
		},
		{
			"surrounding blank lines are trimmed and an empty text is skipped",
			nil,
			[]state.Finding{guidance(1, "\nG1\n"), guidance(2, " \n "), guidance(3, "G2\n")},
			16 * kib, "G1\n\nG2",
			[]int64{1, 3},
		},
		{"nothing pending", nil, nil, 16 * kib, "", nil},
		{"proposed findings alone make no pack", []state.Finding{guidance(1, "P1")}, nil, 16 * kib, "", nil},
		{
			"a pending finding that does not fit after the proposed ones makes no pack",
			[]state.Finding{guidance(1, strings.Repeat("p", 15*kib))},
			[]state.Finding{guidance(2, strings.Repeat("g", 2*kib))},
			16 * kib, "", nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section, ids := packSection(tt.proposed, tt.pending, tt.max)

			assert.Equal(t, tt.section, section)
			assert.Equal(t, tt.ids, ids)
			assert.LessOrEqual(t, len(section), tt.max)
		})
	}
}

func TestTooLong(t *testing.T) {
	fs := []state.Finding{
		guidance(1, "aaaa"), guidance(2, "aaaaa"), guidance(3, "  aaaa\n\n"), guidance(4, "a b c"),
	}

	assert.Equal(t, []state.Finding{fs[1], fs[3]}, tooLong(fs, 4), "trimmed as packed, at the cap fits")
	assert.Nil(t, tooLong(fs, 5))
}

func TestPRTitle(t *testing.T) {
	assert.Equal(t, "Sentinel guidance for alpha", prTitle("alpha"))
}

// bodyLines splits a PR body into its intro line and its other non-blank lines.
func bodyLines(body string) (intro string, rest []string) {
	lines := strings.Split(body, "\n")

	for _, l := range lines[1:] {
		if l != "" {
			rest = append(rest, l)
		}
	}

	return lines[0], rest
}

func TestPRBody(t *testing.T) {
	t.Run("an item per finding with its evidence", func(t *testing.T) {
		long := strings.Repeat("é", 400)
		fs := []state.Finding{
			{ID: 1, Text: "Run make generate before make test.\nIt rebuilds the mocks.", Evidence: "make test failed on a stale mock\nfull log"},
			{ID: 2, Text: long, Evidence: long},
		}

		intro, items := bodyLines(prBody(fs))

		assert.NotEmpty(t, strings.TrimSpace(intro), "one intro line")

		cut := strings.Repeat("é", maxBodyLine)
		assert.Equal(t, []string{
			"- Run make generate before make test.",
			"  Evidence: make test failed on a stale mock",
			"- " + cut,
			"  Evidence: " + cut,
		}, items)
	})

	t.Run("items stop before the agent's limit and the rest are counted", func(t *testing.T) {
		// Item sizes across a range, so some leave less room than the count line needs.
		for size := maxBodyLine - 50; size <= maxBodyLine; size++ {
			fs := make([]state.Finding, 300)
			for i := range fs {
				fs[i] = state.Finding{ID: int64(i + 1), Text: strings.Repeat("t", size), Evidence: strings.Repeat("e", maxBodyLine)}
			}

			body := prBody(fs)
			_, lines := bodyLines(body)

			listed := 0

			for _, l := range lines {
				if strings.HasPrefix(l, "- ") {
					listed++
				}
			}

			require.Positive(t, listed)
			require.Less(t, listed, len(fs))
			assert.LessOrEqual(t, len(body), maxBodyBytes, "item size %d", size)
			assert.Greater(t, len(body)+len("\n- \n  Evidence: ")+size+maxBodyLine, maxBodyBytes, "no room was left for another item")

			last := lines[len(lines)-1]
			assert.False(t, strings.HasPrefix(last, "  Evidence: "), "a last line follows the items")
			assert.Contains(t, last, strconv.Itoa(len(fs)-listed))
		}
	})

	t.Run("a body within the limit has no count line", func(t *testing.T) {
		_, lines := bodyLines(prBody([]state.Finding{{Text: "t", Evidence: "e"}}))

		assert.Equal(t, []string{"- t", "  Evidence: e"}, lines)
	})
}
