package buildinfo

import (
	"encoding/json"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix-protocol/changelog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoFrom_ParsesHistory(t *testing.T) {
	raw := "2026-10-20T21:10:00+02:00\x1fMerge pull request #516 from mhersson/fix/x\x1ffix(web): show the cards\n\x1e\n" +
		"2026-10-20T20:00:00+02:00\x1fMerge pull request #515 from dependabot/npm/foo\x1fbuild(deps): bump foo\n\x1e\n"
	info := infoFrom(fstest.MapFS{"history/merges.log": {Data: []byte(raw)}})

	assert.Equal(t, []protocol.Change{{
		Number: 516, Kind: protocol.ChangeKindFix, Title: "show the cards", MergedAt: "2026-10-20T19:10:00Z",
	}}, info.Changes)
}

func TestInfoFrom_NoHistoryFile(t *testing.T) {
	info := infoFrom(fstest.MapFS{})

	assert.NotNil(t, info.Changes)
	assert.Empty(t, info.Changes)

	out, err := json.Marshal(info)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"changes":[]`)
}

func TestInfoFrom_EmptyHistoryFile(t *testing.T) {
	info := infoFrom(fstest.MapFS{"history/merges.log": {Data: nil}})

	assert.NotNil(t, info.Changes)
	assert.Empty(t, info.Changes)

	out, err := json.Marshal(info)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"changes":[]`)
}

func TestInfoFrom_CopiesStampedVars(t *testing.T) {
	oldV, oldC, oldB := version, commit, buildTime

	t.Cleanup(func() { version, commit, buildTime = oldV, oldC, oldB })

	version, commit, buildTime = "v1.2.3", strings.Repeat("c", 40), "2026-10-21T08:00:00Z"
	info := infoFrom(fstest.MapFS{})

	assert.Equal(t, "v1.2.3", info.Version)
	assert.Equal(t, strings.Repeat("c", 40), info.Commit)
	assert.Equal(t, "2026-10-21T08:00:00Z", info.BuildTime)
}

// The two build files' git log lines feed changelog.Parse and the linker, so
// they are a wire contract.
func TestHistoryCommands_MatchGitLogArgs(t *testing.T) {
	root := os.DirFS("../..")

	for _, file := range []string{"Makefile", ".github/workflows/build.yaml"} {
		t.Run(file, func(t *testing.T) {
			assert.Equal(t, changelog.GitLogArgs, historyCommand(t, root, file))
		})
	}
}

// historyCommand returns the arguments after "git" on the file's one line
// that runs git log, quotes dropped, up to the output redirect.
func historyCommand(t *testing.T, root fs.FS, file string) []string {
	t.Helper()

	raw, err := fs.ReadFile(root, file)
	require.NoError(t, err)

	var lines []string

	for line := range strings.SplitSeq(string(raw), "\n") {
		if strings.Contains(line, "git log --") {
			lines = append(lines, line)
		}
	}

	require.Len(t, lines, 1, "exactly one git log line in %s", file)

	fields := strings.Fields(strings.ReplaceAll(lines[0], "'", ""))
	i := slices.Index(fields, "git")
	require.GreaterOrEqual(t, i, 0)

	args := fields[i+1:]
	if end := slices.IndexFunc(args, func(f string) bool { return strings.HasPrefix(f, ">") }); end >= 0 {
		args = args[:end]
	}

	return args
}
