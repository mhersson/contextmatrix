// Package gitops provides git operations for auto-committing card mutations.
// The git repository is the boards directory itself (cfg.BoardsDir).
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
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// DefaultAuthor is used when no author is configured.
var DefaultAuthor = object.Signature{
	Name:  "ContextMatrix",
	Email: "contextmatrix@local",
}

// Manager handles git operations for the board repository.
// All operations are mutex-protected to ensure serialized access.
type Manager struct {
	repo     *git.Repository
	repoPath string
	author   object.Signature
	authMode string
	token    string
	mu       sync.Mutex
}

// NewManager opens an existing git repository or initializes a new one.
// The repoPath should be the boards directory (cfg.BoardsDir).
// If cloneURL is non-empty and no repository exists at repoPath, it will
// be cloned from the given URL instead of initialized as empty.
// authMode and token configure PAT authentication for shell git operations:
// use "ssh" (or "") to preserve the default environment, or "pat" to inject
// an Authorization Bearer header via GIT_CONFIG_* env vars.
func NewManager(repoPath string, cloneURL string, authMode string, token string) (*Manager, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	var repo *git.Repository

	// Try to open existing repository
	repo, err = git.PlainOpen(absPath)
	if err != nil {
		if !errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, fmt.Errorf("open repository: %w", err)
		}

		if cloneURL != "" {
			// Clone from remote with the configured auth env.
			repo, err = cloneRepo(absPath, cloneURL, GitAuthEnv(authMode, token))
		} else {
			// Initialize new empty repository
			slog.Info("initializing new git repository", "path", absPath)
			repo, err = git.PlainInit(absPath, false)
		}
		if err != nil {
			return nil, err
		}
	}

	mgr := &Manager{
		repo:     repo,
		repoPath: absPath,
		author:   DefaultAuthor,
		authMode: authMode,
		token:    token,
	}

	// If a remote URL is provided but the repo was opened (not cloned),
	// ensure the origin remote is configured for auto_push/auto_pull.
	if cloneURL != "" && !mgr.hasRemote("origin") {
		if addErr := mgr.AddRemote("origin", cloneURL); addErr != nil {
			slog.Warn("failed to add origin remote", "error", addErr)
		}
	}

	return mgr, nil
}

// cloneRepo clones a git repository from the given URL into the target directory
// using shell git (consistent with push/pull operations).
// authEnv contains additional environment variables (e.g. GIT_CONFIG_* for PAT
// auth) to pass to the git subprocess; nil preserves the default environment.
func cloneRepo(targetDir, url string, authEnv []string) (*git.Repository, error) {
	slog.Info("cloning boards repository", "url", url, "target", targetDir)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "clone", url, targetDir)
	if len(authEnv) > 0 {
		cmd.Env = append(os.Environ(), authEnv...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		return nil, fmt.Errorf("clone repository: %s: %s", err, output)
	}

	repo, err := git.PlainOpen(targetDir)
	if err != nil {
		return nil, fmt.Errorf("open cloned repository: %w", err)
	}

	slog.Info("boards repository cloned successfully", "path", targetDir)
	return repo, nil
}

// SetAuthor configures the commit author for all commits.
func (m *Manager) SetAuthor(name, email string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.author = object.Signature{Name: name, Email: email}
}

// CommitFile stages a specific file and commits it.
// The path should be relative to the repository root.
func (m *Manager) CommitFile(path, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Stage the specific file
	_, err = wt.Add(path)
	if err != nil {
		return fmt.Errorf("stage file %s: %w", path, err)
	}

	// Commit
	author := m.author
	author.When = time.Now()

	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &author,
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// CommitFiles stages specific files and commits them in a single commit.
// The paths should be relative to the repository root.
func (m *Manager) CommitFiles(paths []string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Stage each file
	for _, path := range paths {
		if _, err := wt.Add(path); err != nil {
			return fmt.Errorf("stage file %s: %w", path, err)
		}
	}

	// Commit
	author := m.author
	author.When = time.Now()

	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &author,
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// CommitAll stages all changes and commits them.
func (m *Manager) CommitAll(message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Stage all changes
	err = wt.AddGlob(".")
	if err != nil {
		return fmt.Errorf("stage all: %w", err)
	}

	// Commit
	author := m.author
	author.When = time.Now()

	_, err = wt.Commit(message, &git.CommitOptions{
		Author: &author,
	})
	if err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// Pull fetches and rebases from the origin remote using shell git.
// Returns nil if no remote is configured (with a warning logged).
func (m *Manager) Pull(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hasRemote("origin") {
		slog.Warn("no remote 'origin' configured, skipping pull")
		return nil
	}

	if err := m.runGit(ctx, "pull", "--rebase", "origin"); err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	if err := m.reloadRepo(); err != nil {
		return fmt.Errorf("reload after pull: %w", err)
	}

	return nil
}

// Push pushes commits to the origin remote using shell git.
// Uses "git push --set-upstream origin HEAD" so it works whether or not
// the current branch already has a tracking upstream configured.
// Returns nil if no remote is configured (with a warning logged).
func (m *Manager) Push(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hasRemote("origin") {
		slog.Warn("no remote 'origin' configured, skipping push")
		return nil
	}

	if err := m.runGit(ctx, "push", "--set-upstream", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	if err := m.reloadRepo(); err != nil {
		return fmt.Errorf("reload after push: %w", err)
	}

	return nil
}

// CommitFilesShell stages specific files and commits them using shell git.
// Unlike CommitFiles (which uses go-git), this method is immune to stale
// in-memory state after shell-based push/rebase operations.
// Returns nil without committing if no files have staged changes.
func (m *Manager) CommitFilesShell(ctx context.Context, paths []string, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stage each file.
	args := append([]string{"add", "--"}, paths...)
	if err := m.runGit(ctx, args...); err != nil {
		return fmt.Errorf("stage files: %w", err)
	}

	// Check if anything was actually staged. `git diff --cached --quiet`
	// exits 0 when there are no staged changes.
	if err := m.runGit(ctx, "diff", "--cached", "--quiet"); err == nil {
		return nil // nothing to commit
	}

	// Commit with explicit author to match go-git commits.
	author := fmt.Sprintf("%s <%s>", m.author.Name, m.author.Email)
	if err := m.runGit(ctx, "commit", "--author", author, "-m", message); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// ReloadRepo re-opens the go-git repository from disk, refreshing any
// stale in-memory state caused by shell git operations (push, rebase).
func (m *Manager) ReloadRepo() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.reloadRepo()
}

// reloadRepo is the lock-free implementation of ReloadRepo.
// Must be called with mu held.
func (m *Manager) reloadRepo() error {
	repo, err := git.PlainOpen(m.repoPath)
	if err != nil {
		return fmt.Errorf("reload repository: %w", err)
	}
	m.repo = repo
	return nil
}

// runGit executes a git command in the repository directory.
// Must be called without mu held (or from a context that already holds it,
// as this method does not re-acquire the lock).
// Auth environment variables (e.g. GIT_CONFIG_* for PAT) are appended
// automatically based on the Manager's configured authMode and token.
func (m *Manager) runGit(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.repoPath

	if env := GitAuthEnv(m.authMode, m.token); len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	slog.Debug("gitops: running", "cmd", "git "+strings.Join(args, " "), "dir", m.repoPath)

	if err := cmd.Run(); err != nil {
		output := strings.TrimSpace(stderr.String())
		if output == "" {
			output = strings.TrimSpace(stdout.String())
		}
		return fmt.Errorf("%s: %s", err, output)
	}

	return nil
}

// AddRemote adds a remote to the repository.
func (m *Manager) AddRemote(name, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_, err := m.repo.CreateRemote(&config.RemoteConfig{
		Name: name,
		URLs: []string{url},
	})
	if err != nil {
		return fmt.Errorf("create remote: %w", err)
	}

	return nil
}

// hasRemote checks if a remote with the given name exists.
// Must be called with mu held.
func (m *Manager) hasRemote(name string) bool {
	remotes, err := m.repo.Remotes()
	if err != nil {
		return false
	}

	for _, r := range remotes {
		if r.Config().Name == name {
			return true
		}
	}
	return false
}

// RepoPath returns the absolute path to the repository root.
func (m *Manager) RepoPath() string {
	return m.repoPath
}

// HasRemote reports whether the repository has an "origin" remote configured.
func (m *Manager) HasRemote() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hasRemote("origin")
}

// CurrentBranch returns the short name of the currently checked-out branch.
func (m *Manager) CurrentBranch() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	head, err := m.repo.Head()
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}
	return head.Name().Short(), nil
}

// HasUncommittedChanges checks if there are staged or unstaged changes.
func (m *Manager) HasUncommittedChanges() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("get worktree: %w", err)
	}

	status, err := wt.Status()
	if err != nil {
		return false, fmt.Errorf("get status: %w", err)
	}

	return !status.IsClean(), nil
}

// GetLastCommitMessage returns the message of the most recent commit.
// Returns empty string if no commits exist.
func (m *Manager) GetLastCommitMessage() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	head, err := m.repo.Head()
	if err != nil {
		// No commits yet
		return "", nil
	}

	commit, err := m.repo.CommitObject(head.Hash())
	if err != nil {
		return "", fmt.Errorf("get commit: %w", err)
	}

	return commit.Message, nil
}

// CommitCount returns the total number of commits in the repository.
// Returns 0 if no commits exist.
func (m *Manager) CommitCount() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	head, err := m.repo.Head()
	if err != nil {
		return 0, nil // no commits
	}

	iter, err := m.repo.Log(&git.LogOptions{From: head.Hash()})
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}

	count := 0
	_ = iter.ForEach(func(_ *object.Commit) error {
		count++
		return nil
	})

	return count, nil
}

// DeleteFile removes a file from the working tree and stages the deletion.
func (m *Manager) DeleteFile(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Remove the file from the filesystem
	fullPath := filepath.Join(m.repoPath, path)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove file: %w", err)
	}

	// Stage the deletion
	_, err = wt.Remove(path)
	if err != nil {
		return fmt.Errorf("stage deletion %s: %w", path, err)
	}

	return nil
}
