package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ErrMergeConflict is returned by Merge when git stopped with unmerged paths
// left in the index. The caller is expected to resolve them and call
// CommitMerge, or to call MergeAbort.
var ErrMergeConflict = errors.New("merge conflict")

// UnmergedPath is one conflicted index entry. The three flags report which
// of the merge stages git recorded: base (stage 1), ours (stage 2), theirs
// (stage 3). A missing stage means that side added or deleted the path.
type UnmergedPath struct {
	Path                        string
	HasBase, HasOurs, HasTheirs bool
}

// runGitOutput is runGit with stdout returned to the caller. Every user of it
// is a local operation (index, worktree, or object database), so it does not
// build the network auth environment. Callers hold worktreeMu.
func (m *Manager) runGitOutput(ctx context.Context, args ...string) (string, error) {
	// Match runGit: under `go test`, disable GPG signing so the suite is
	// hermetic regardless of the developer's global git config.
	if testing.Testing() {
		args = append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.repoPath
	cmd.WaitDelay = 3 * time.Second

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("gitops: running", "cmd", "git "+strings.Join(args, " "), "dir", m.repoPath)

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		if output == "" {
			output = strings.TrimSpace(stdout.String())
		}

		return stdout.String(), fmt.Errorf("git %s: %w (%s)", args[0], err, output)
	}

	return stdout.String(), nil
}

// IsClean reports whether the worktree and index match HEAD. The second
// return value lists the paths that make it dirty, for logging.
func (m *Manager) IsClean(ctx context.Context) (bool, []string, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, nil, fmt.Errorf("status: %w", err)
	}

	var paths []string

	for _, line := range splitLines(out) {
		// Porcelain v1 lines are "XY <path>"; anything shorter carries no path.
		if len(line) > 3 {
			paths = append(paths, strings.TrimSpace(line[3:]))
		}
	}

	return len(paths) == 0, paths, nil
}

func (m *Manager) MergeBase(ctx context.Context, ref string) (string, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "merge-base", "HEAD", ref)
	if err != nil {
		return "", fmt.Errorf("merge-base: %w", err)
	}

	return strings.TrimSpace(out), nil
}

func (m *Manager) DiffNames(ctx context.Context, from, to string) ([]string, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "diff", "--name-only", from, to)
	if err != nil {
		return nil, fmt.Errorf("diff names: %w", err)
	}

	return splitLines(out), nil
}

// MergeFastForward advances HEAD to ref when the local branch is an ancestor
// of it. It never creates a commit and never rewrites history; a divergent
// branch returns an error and the caller falls back to Merge.
func (m *Manager) MergeFastForward(ctx context.Context, ref string) error {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	if err := m.runGit(ctx, "merge", "--ff-only", ref); err != nil {
		return fmt.Errorf("fast-forward: %w", err)
	}

	return m.reloadRepo()
}

// Merge performs a true merge of ref into HEAD, always producing a merge
// commit on success. Returns ErrMergeConflict when git stopped with unmerged
// index entries, leaving the merge in progress for the caller to resolve or
// abort; any other failure is wrapped.
func (m *Manager) Merge(ctx context.Context, ref string) error {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	mergeErr := m.runGit(ctx,
		"-c", "user.name="+m.author.Name,
		"-c", "user.email="+m.author.Email,
		"merge", "--no-edit", "--no-ff", ref)
	if mergeErr == nil {
		return m.reloadRepo()
	}

	out, err := m.runGitOutput(ctx, "ls-files", "-u")
	if err == nil && strings.TrimSpace(out) != "" {
		return ErrMergeConflict
	}

	return fmt.Errorf("merge: %w", mergeErr)
}

func (m *Manager) MergeAbort(ctx context.Context) error {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	if err := m.runGit(ctx, "merge", "--abort"); err != nil {
		return fmt.Errorf("merge abort: %w", err)
	}

	return m.reloadRepo()
}

// MergeInProgress reports whether a merge is waiting for resolution. The
// board repository is a plain clone, so MERGE_HEAD always lives in .git.
func (m *Manager) MergeInProgress() bool {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	_, err := os.Stat(filepath.Join(m.repoPath, ".git", "MERGE_HEAD"))

	return err == nil
}

// UnmergedPaths lists the conflicted index entries in index order, one entry
// per path with a flag per recorded stage.
func (m *Manager) UnmergedPaths(ctx context.Context) ([]UnmergedPath, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	// -z is mandatory: paths may contain spaces, and the non-z form quotes
	// and escapes them instead of emitting the raw bytes.
	out, err := m.runGitOutput(ctx, "ls-files", "-u", "-z")
	if err != nil {
		return nil, fmt.Errorf("ls-files unmerged: %w", err)
	}

	byPath := map[string]*UnmergedPath{}

	var order []string

	for _, rec := range strings.Split(out, "\x00") {
		if rec == "" {
			continue
		}

		// Record layout: "<mode> <sha> <stage>\t<path>".
		meta, path, ok := strings.Cut(rec, "\t")
		if !ok {
			continue
		}

		fields := strings.Fields(meta)
		if len(fields) != 3 {
			continue
		}

		u := byPath[path]
		if u == nil {
			u = &UnmergedPath{Path: path}
			byPath[path] = u
			order = append(order, path)
		}

		switch fields[2] {
		case "1":
			u.HasBase = true
		case "2":
			u.HasOurs = true
		case "3":
			u.HasTheirs = true
		}
	}

	result := make([]UnmergedPath, 0, len(order))
	for _, p := range order {
		result = append(result, *byPath[p])
	}

	return result, nil
}

// ShowStage returns the blob recorded for path at the given merge stage
// (1 base, 2 ours, 3 theirs). Returns nil, nil when that stage is absent,
// which is how git records a path one side added or deleted.
func (m *Manager) ShowStage(ctx context.Context, stage int, path string) ([]byte, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "show", fmt.Sprintf(":%d:%s", stage, path))
	if err != nil {
		if isMissingStage(err) {
			return nil, nil
		}

		return nil, fmt.Errorf("show stage %d %s: %w", stage, path, err)
	}

	return []byte(out), nil
}

// isMissingStage distinguishes "this stage does not exist" from a real git
// failure. The first two messages are what git prints for an untracked path
// and for a tracked path with no entry at the requested stage; the third
// covers older versions that report it as a bad object name.
func isMissingStage(err error) bool {
	msg := err.Error()

	return strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "is in the index, but not at stage") ||
		strings.Contains(msg, "invalid object name")
}

func (m *Manager) StagePaths(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	if err := m.runGit(ctx, append([]string{"add", "--"}, paths...)...); err != nil {
		return fmt.Errorf("stage: %w", err)
	}

	return nil
}

// RemovePaths drops paths from the index and deletes them from the worktree.
// Both halves tolerate an already-absent path so the caller can apply an
// upstream deletion without first checking what is still present.
func (m *Manager) RemovePaths(ctx context.Context, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	args := append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, paths...)
	if err := m.runGit(ctx, args...); err != nil {
		return fmt.Errorf("unstage: %w", err)
	}

	for _, p := range paths {
		if err := os.Remove(filepath.Join(m.repoPath, p)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}

	return nil
}

// CommitMerge concludes a merge whose conflicts the caller has staged. The
// author and committer identity match CommitFilesShell so merge commits are
// indistinguishable from ordinary board commits.
func (m *Manager) CommitMerge(ctx context.Context, message string) error {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	author := fmt.Sprintf("%s <%s>", m.author.Name, m.author.Email)

	if err := m.runGit(ctx,
		"-c", "user.name="+m.author.Name,
		"-c", "user.email="+m.author.Email,
		"commit", "--no-edit", "--author", author, "-m", message); err != nil {
		return fmt.Errorf("commit merge: %w", err)
	}

	return m.reloadRepo()
}

// AheadCount returns how many commits HEAD carries that origin/branch does
// not, i.e. what a push would send.
func (m *Manager) AheadCount(ctx context.Context, branch string) (int, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "rev-list", "--count", "origin/"+branch+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("ahead count: %w", err)
	}

	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("ahead count parse: %w", err)
	}

	return n, nil
}

func (m *Manager) RevParseShort(ctx context.Context, ref string) (string, error) {
	m.worktreeMu.Lock()
	defer m.worktreeMu.Unlock()

	out, err := m.runGitOutput(ctx, "rev-parse", "--short", ref)
	if err != nil {
		return "", fmt.Errorf("rev-parse: %w", err)
	}

	return strings.TrimSpace(out), nil
}

// MergeFileText three-way merges the text of a single file through
// `git merge-file -p`, without touching the repository. clean is false when
// git reported conflicts; merged then holds the conflict-marked text.
func MergeFileText(ctx context.Context, base, ours, theirs string) (string, bool, error) {
	dir, err := os.MkdirTemp("", "cm-merge-*")
	if err != nil {
		return "", false, fmt.Errorf("temp dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(dir) }()

	basePath := filepath.Join(dir, "base")
	oursPath := filepath.Join(dir, "ours")
	theirsPath := filepath.Join(dir, "theirs")

	for path, content := range map[string]string{basePath: base, oursPath: ours, theirsPath: theirs} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return "", false, fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
	}

	cmd := exec.CommandContext(ctx, "git", "merge-file", "-p",
		"-L", "local", "-L", "base", "-L", "remote",
		oursPath, basePath, theirsPath)
	cmd.WaitDelay = 3 * time.Second

	var stdout bytes.Buffer

	cmd.Stdout = &stdout

	runErr := cmd.Run()

	var exit *exec.ExitError

	switch {
	case runErr == nil:
		return stdout.String(), true, nil
	case errors.As(runErr, &exit) && exit.ExitCode() > 0 && exit.ExitCode() < 128:
		// git merge-file exits with the number of conflicts it left behind.
		return stdout.String(), false, nil
	default:
		return "", false, fmt.Errorf("merge-file: %w", runErr)
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}

	return strings.Split(s, "\n")
}
