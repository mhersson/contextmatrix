package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readLines(t *testing.T, path string) []string {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	trimmed := strings.TrimSuffix(string(data), "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}

func newTestLog(t *testing.T, path string, maxBytes int64, keep int) *AuditLog {
	t.Helper()

	l, err := NewAuditLog(path, maxBytes, keep)
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })

	return l
}

func TestNewAuditLog_Creates0600WithParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "knowledge.log")
	newTestLog(t, path, DefaultAuditMaxBytes, DefaultAuditKeep)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestAuditLog_WritesOneJSONLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")
	l := newTestLog(t, path, DefaultAuditMaxBytes, DefaultAuditKeep)

	answer := json.RawMessage(`{"items": [ {"id":"a"} ]}`)

	require.NoError(t, l.Write(Entry{
		Time: time.Now(), Provider: "p", Scope: "s", Project: "proj", Caller: "c",
		Tool: "knowledge_query", Arguments: map[string]any{"query": "q"},
		Answer: answer, DurationMS: 12,
	}))
	require.NoError(t, l.Write(Entry{
		Time: time.Now(), Provider: "p", Tool: "knowledge_item", Error: "boom",
	}))

	lines := readLines(t, path)
	require.Len(t, lines, 2)

	var first map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &first))

	for _, k := range []string{"time", "provider", "scope", "project", "caller", "tool", "arguments", "answer", "duration_ms"} {
		assert.Contains(t, first, k)
	}

	assert.NotContains(t, first, "cached")
	assert.NotContains(t, first, "error")

	var want bytes.Buffer
	require.NoError(t, json.Compact(&want, answer))
	assert.Equal(t, want.String(), string(first["answer"]))

	var second map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &second))
	assert.Contains(t, second, "error")
	assert.Equal(t, "null", string(second["answer"]))
	assert.NotContains(t, second, "cached")
}

func TestAuditLog_AppendsToExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")

	l, err := NewAuditLog(path, DefaultAuditMaxBytes, DefaultAuditKeep)
	require.NoError(t, err)
	require.NoError(t, l.Write(Entry{Provider: "one"}))
	require.NoError(t, l.Close())

	l = newTestLog(t, path, DefaultAuditMaxBytes, DefaultAuditKeep)
	require.NoError(t, l.Write(Entry{Provider: "two"}))

	assert.Len(t, readLines(t, path), 2)
}

func TestAuditLog_RotatesAndKeepsN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")

	// Every entry marshals to the same length, so a file holds exactly three
	// lines: the fourth would take it past maxBytes and rotates first.
	line, err := json.Marshal(Entry{Provider: "p0"})
	require.NoError(t, err)

	maxBytes := 3 * int64(len(line)+1)
	l := newTestLog(t, path, maxBytes, 2)

	for i := range 10 {
		require.NoError(t, l.Write(Entry{Provider: fmt.Sprintf("p%d", i)}))
	}

	for _, p := range []string{path, path + ".1", path + ".2"} {
		info, err := os.Stat(p)
		require.NoError(t, err, p)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), p)
		assert.LessOrEqual(t, info.Size(), maxBytes, p)
	}

	_, err = os.Stat(path + ".3")
	assert.True(t, os.IsNotExist(err))

	provider := func(p string) []string {
		var out []string

		for _, line := range readLines(t, p) {
			var e Entry
			require.NoError(t, json.Unmarshal([]byte(line), &e))

			out = append(out, e.Provider)
		}

		return out
	}

	assert.Equal(t, []string{"p9"}, provider(path))
	assert.Equal(t, []string{"p6", "p7", "p8"}, provider(path+".1"))
	assert.Equal(t, []string{"p3", "p4", "p5"}, provider(path+".2"))
}

func TestAuditLog_FailedRotationStillWritesTheLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")
	l := newTestLog(t, path, 50, 1)

	// A non-empty directory where the rotated file belongs makes every
	// rotation fail while the log itself can still be reopened.
	require.NoError(t, os.MkdirAll(filepath.Join(path+".1", "blocker"), 0o700))

	require.NoError(t, l.Write(Entry{Provider: "p0"}))

	for _, p := range []string{"p1", "p2"} {
		require.Error(t, l.Write(Entry{Provider: p}), p)
	}

	var got []string

	for _, line := range readLines(t, path) {
		var e Entry
		require.NoError(t, json.Unmarshal([]byte(line), &e))

		got = append(got, e.Provider)
	}

	assert.Equal(t, []string{"p0", "p1", "p2"}, got)
}

func TestAuditLog_OversizedEntryWrittenWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")
	l := newTestLog(t, path, 50, 1)

	require.NoError(t, l.Write(Entry{Provider: strings.Repeat("y", 300)}))

	lines := readLines(t, path)
	require.Len(t, lines, 1)

	var e Entry
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &e))
}

func TestAuditLog_MarshalErrorLeavesNoPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")
	l := newTestLog(t, path, DefaultAuditMaxBytes, DefaultAuditKeep)

	require.Error(t, l.Write(Entry{Arguments: map[string]any{"bad": make(chan int)}}))
	assert.Empty(t, readLines(t, path))
}

func TestAuditLog_ConcurrentWritesStayWhole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge.log")
	l := newTestLog(t, path, DefaultAuditMaxBytes, DefaultAuditKeep)

	var wg sync.WaitGroup

	for g := range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range 50 {
				assert.NoError(t, l.Write(Entry{Provider: fmt.Sprintf("g%d", g), Tool: "knowledge_query"}))
			}
		}()
	}

	wg.Wait()

	lines := readLines(t, path)
	assert.Len(t, lines, 400)

	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)))
	}
}
