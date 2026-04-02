// Package gitops provides git operations for auto-committing card mutations.
// The git repository is the boards directory itself (cfg.BoardsDir).
package gitops

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
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
	mu       sync.Mutex
}

// NewManager opens an existing git repository or initializes a new one.
// The repoPath should be the boards directory (cfg.BoardsDir).
func NewManager(repoPath string) (*Manager, error) {
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

		// Initialize new repository
		slog.Info("initializing new git repository", "path", absPath)
		repo, err = git.PlainInit(absPath, false)
		if err != nil {
			return nil, fmt.Errorf("init repository: %w", err)
		}
	}

	return &Manager{
		repo:     repo,
		repoPath: absPath,
		author:   DefaultAuthor,
	}, nil
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

// Pull fetches and merges from the origin remote.
// Returns nil if no remote is configured (with a warning logged).
func (m *Manager) Pull() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wt, err := m.repo.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}

	// Check if origin remote exists
	if !m.hasRemote("origin") {
		slog.Warn("no remote 'origin' configured, skipping pull")
		return nil
	}

	err = wt.Pull(&git.PullOptions{
		RemoteName: "origin",
	})
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil
		}
		if errors.Is(err, transport.ErrEmptyRemoteRepository) {
			slog.Warn("remote repository is empty, skipping pull")
			return nil
		}
		return fmt.Errorf("pull: %w", err)
	}

	return nil
}

// Push pushes commits to the origin remote.
// Returns nil if no remote is configured (with a warning logged).
func (m *Manager) Push() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if origin remote exists
	if !m.hasRemote("origin") {
		slog.Warn("no remote 'origin' configured, skipping push")
		return nil
	}

	err := m.repo.Push(&git.PushOptions{
		RemoteName: "origin",
	})
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil
		}
		return fmt.Errorf("push: %w", err)
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
