package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mhersson/contextmatrix/internal/ctxlog"
)

// TaskSkillSummary is the API representation of an available task-skill,
// returned by GET /api/task-skills for use by the project-default and
// per-card skill selectors in the web UI.
type TaskSkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// taskSkillNamePattern restricts skill directory names to a safe charset
// that cannot reach outside the task-skills mount via path traversal.
// Mirrors the runner-side ValidateTaskSkills check and the service-layer
// validateSkillNames pattern.
var taskSkillNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// taskSkillsLister discovers task-skills from a filesystem directory and
// caches the result by directory mtime. Safe for concurrent use.
//
// Caching by mtime is good enough for the boards-side workflow: editors
// drop SKILL.md files in via scripts/install.sh or git pull and the
// directory mtime updates accordingly. The cache is invalidated as soon
// as the directory is touched.
type taskSkillsLister struct {
	dir string

	mu          sync.Mutex
	cache       []TaskSkillSummary
	cachedMtime time.Time
}

// newTaskSkillsLister returns a lister rooted at dir. An empty dir
// produces a no-op lister (List returns nil, nil) so the endpoint is
// safe to wire even on installations that haven't configured
// task_skills.dir.
func newTaskSkillsLister(dir string) *taskSkillsLister {
	return &taskSkillsLister{dir: dir}
}

// List returns the task-skills sorted by name. Skills with missing or
// malformed SKILL.md are skipped (not surfaced as errors) so a single
// broken skill does not break the whole listing.
func (l *taskSkillsLister) List(ctx context.Context) ([]TaskSkillSummary, error) {
	if l.dir == "" {
		return nil, nil
	}

	info, err := os.Stat(l.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}

		return nil, fmt.Errorf("stat task-skills dir: %w", err)
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("task-skills path is not a directory: %s", l.dir)
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.cachedMtime.IsZero() && info.ModTime().Equal(l.cachedMtime) {
		return cloneSkills(l.cache), nil
	}

	skills, err := readTaskSkillsDir(ctx, l.dir)
	if err != nil {
		return nil, err
	}

	l.cache = skills
	l.cachedMtime = info.ModTime()

	return cloneSkills(skills), nil
}

// Names returns just the skill names from the current cache (refreshing
// if needed). Used by validators that only need set-membership checks.
//
// Returns a nil map when the lister is unconfigured (no task_skills.dir),
// so callers can distinguish "unconfigured, skip validation" from
// "configured but empty, reject everything".
func (l *taskSkillsLister) Names(ctx context.Context) (map[string]struct{}, error) {
	if l.dir == "" {
		return nil, nil
	}

	skills, err := l.List(ctx)
	if err != nil {
		return nil, err
	}

	names := make(map[string]struct{}, len(skills))
	for _, s := range skills {
		names[s.Name] = struct{}{}
	}

	return names, nil
}

// readTaskSkillsDir scans dir for subdirectories containing SKILL.md and
// returns their names + descriptions sorted by name. Subdirectories whose
// names fail the safety pattern, or whose SKILL.md is missing/malformed,
// are silently skipped.
func readTaskSkillsDir(ctx context.Context, dir string) ([]TaskSkillSummary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read task-skills dir: %w", err)
	}

	out := make([]TaskSkillSummary, 0, len(entries))
	logger := ctxlog.Logger(ctx)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		if !taskSkillNamePattern.MatchString(name) {
			logger.Debug("skipping task-skill with invalid name", "name", name)

			continue
		}

		path := filepath.Join(dir, name, "SKILL.md")

		data, err := os.ReadFile(path) //nolint:gosec // path is under our configured task_skills.dir
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				logger.Warn("failed to read SKILL.md", "skill", name, "error", err)
			}

			continue
		}

		desc, err := parseSkillDescription(data)
		if err != nil {
			logger.Warn("failed to parse SKILL.md frontmatter", "skill", name, "error", err)

			continue
		}

		out = append(out, TaskSkillSummary{Name: name, Description: desc})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out, nil
}

// parseSkillDescription extracts the `description` field from a SKILL.md's
// YAML frontmatter. The frontmatter must be delimited by leading and
// trailing `---` lines.
func parseSkillDescription(data []byte) (string, error) {
	// Strip UTF-8 BOM if present (EF BB BF).
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}

	s := string(data)

	switch {
	case strings.HasPrefix(s, "---\n"):
		s = s[4:]
	case strings.HasPrefix(s, "---\r\n"):
		s = s[5:]
	default:
		return "", errors.New("missing frontmatter delimiter")
	}

	// Find closing delimiter on its own line.
	end := -1

	for _, sep := range []string{"\n---\n", "\n---\r\n", "\n---"} {
		if i := strings.Index(s, sep); i >= 0 {
			end = i

			break
		}
	}

	if end < 0 {
		return "", errors.New("unterminated frontmatter")
	}

	body := s[:end]

	var fm struct {
		Description string `yaml:"description"`
	}

	if err := yaml.Unmarshal([]byte(body), &fm); err != nil {
		return "", fmt.Errorf("parse frontmatter: %w", err)
	}

	return fm.Description, nil
}

// cloneSkills returns a fresh slice so callers can't mutate the cache.
func cloneSkills(in []TaskSkillSummary) []TaskSkillSummary {
	if in == nil {
		return nil
	}

	out := make([]TaskSkillSummary, len(in))
	copy(out, in)

	return out
}

// taskSkillHandlers contains handlers for task-skills endpoints.
type taskSkillHandlers struct {
	lister *taskSkillsLister
}

// listTaskSkills handles GET /api/task-skills.
//
// Returns the task-skills available in the configured task_skills.dir,
// sorted by name. If the directory is unconfigured or empty, returns
// {"skills": []}.
func (h *taskSkillHandlers) listTaskSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := h.lister.List(r.Context())
	if err != nil {
		ctxlog.Logger(r.Context()).Error("list task-skills failed", "error", err)
		writeError(w, http.StatusInternalServerError, ErrCodeInternalError, "failed to list task skills", "")

		return
	}

	if skills == nil {
		skills = []TaskSkillSummary{}
	}

	writeJSON(w, http.StatusOK, struct {
		Skills []TaskSkillSummary `json:"skills"`
	}{Skills: skills})
}

// validateSkillsAgainstAvailable returns an error listing any names in
// `skills` that are not present in `available`. A nil or empty `skills`
// slice is always valid (mounting nothing is allowed). When `available`
// is nil (the lister returned no skills, e.g. unconfigured), validation
// is skipped — the runner-side check is the final guard.
func validateSkillsAgainstAvailable(skills []string, available map[string]struct{}) error {
	if len(skills) == 0 || available == nil {
		return nil
	}

	var unknown []string

	for _, s := range skills {
		if _, ok := available[s]; !ok {
			unknown = append(unknown, s)
		}
	}

	if len(unknown) > 0 {
		return fmt.Errorf("unknown task-skills: %s", strings.Join(unknown, ", "))
	}

	return nil
}

// validateSkillsAgainstProjectDefault returns an error listing any names
// in `skills` that are not present in `projectDefault`. Used for the
// per-card subset constraint: when a project has default_skills set
// (non-nil), each card's skills must be a subset of that.
//
// If projectDefault is nil (no project-level constraint), this passes —
// the per-card list can include any skill from the global available set.
// If projectDefault is empty (mount nothing), and `skills` is non-empty,
// the violation is reported.
func validateSkillsAgainstProjectDefault(skills []string, projectDefault *[]string) error {
	if projectDefault == nil || len(skills) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(*projectDefault))
	for _, s := range *projectDefault {
		allowed[s] = struct{}{}
	}

	var outside []string

	for _, s := range skills {
		if _, ok := allowed[s]; !ok {
			outside = append(outside, s)
		}
	}

	if len(outside) > 0 {
		return fmt.Errorf("task-skills not in project default_skills: %s", strings.Join(outside, ", "))
	}

	return nil
}
