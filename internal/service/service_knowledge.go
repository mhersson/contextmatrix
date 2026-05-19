package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/refresh"
	"github.com/mhersson/contextmatrix/internal/storage"
)

// errNoReposConfigured is returned by resolveRepo when a project has an
// empty effective repos list. ReadKnowledgeBase treats this as "no KB
// available, return empty result"; other callers propagate it.
var errNoReposConfigured = errors.New("project has no repos configured")

// setKnowledgeCommitFnForTest replaces the commit function used by
// WriteKnowledgeDocs so tests can inject deterministic failures.
func (s *CardService) setKnowledgeCommitFnForTest(fn func(ctx context.Context, paths []string, message string) error) {
	s.knowledgeCommitFn = fn
}

// KnowledgeWriteSource identifies the origin of a KB write so the service
// can vary human_edited flag handling and the commit message.
type KnowledgeWriteSource int

const (
	KnowledgeWriteSourceRefresh KnowledgeWriteSource = iota
	KnowledgeWriteSourceEdit
)

// WriteKnowledgeDocsInput carries the parameters for WriteKnowledgeDocs.
type WriteKnowledgeDocsInput struct {
	Project    string
	Repo       string
	Docs       map[string]string // doc name -> markdown content
	HeadCommit string            // target repo HEAD SHA at refresh time (Refresh only)
	Source     KnowledgeWriteSource
	AgentID    string // for last_built_by
}

// WriteKnowledgeDocsResult is returned by a successful WriteKnowledgeDocs call.
type WriteKnowledgeDocsResult struct {
	FilesWritten []string
}

// WriteKnowledgeDocs writes one or more KB docs in a single atomic git commit.
// Updates .meta.yaml to reflect new build state per doc. The human_edited
// flag is set to true when Source==Edit and false when Source==Refresh.
func (s *CardService) WriteKnowledgeDocs(ctx context.Context, in WriteKnowledgeDocsInput) (*WriteKnowledgeDocsResult, error) {
	if in.Project == "" || in.Repo == "" {
		return nil, fmt.Errorf("%w: project and repo required", storage.ErrInvalidInput)
	}

	if len(in.Docs) == 0 {
		return nil, fmt.Errorf("%w: docs map empty", storage.ErrInvalidInput)
	}

	if in.Source == KnowledgeWriteSourceRefresh {
		if in.AgentID == "" {
			return nil, fmt.Errorf("agent_id required for refresh writes")
		}

		if !board.IsHumanAgentID(in.AgentID) {
			return nil, fmt.Errorf("agent_id must start with \"human:\" and have a non-empty suffix - refresh is human-only")
		}

		if in.HeadCommit == "" {
			return nil, fmt.Errorf("head_commit required for refresh writes")
		}
	}

	for name := range in.Docs {
		if !board.IsValidKnowledgeDoc(name) {
			return nil, fmt.Errorf("%w: %q", storage.ErrInvalidKnowledgeDoc, name)
		}
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	// Verify project exists.
	cfg, err := s.store.GetProject(ctx, in.Project)
	if err != nil {
		return nil, err
	}

	// Look up the repo URL from project config (best-effort; tests may have empty repos).
	var repoURL string

	for _, r := range cfg.EffectiveRepos() {
		if r.Name == in.Repo {
			repoURL = r.URL

			break
		}
	}

	// Read current meta.
	meta, err := s.store.ReadKnowledgeMeta(ctx, in.Project)
	if err != nil {
		return nil, err
	}

	if meta.Repos == nil {
		meta.Repos = map[string]board.KnowledgeRepoMeta{}
	}

	repoMeta := meta.Repos[in.Repo]
	if repoMeta.Docs == nil {
		repoMeta.Docs = map[string]board.KnowledgeDocMeta{}
	}

	// Always refresh URL from project config (authoritative); meta is a cache.
	if repoURL != "" {
		repoMeta.URL = repoURL
	}

	// Snapshot current on-disk state before any writes so we can roll back
	// on commit failure. priorBytes[name]==nil means the doc did not exist.
	priorBytes := make(map[string][]byte, len(in.Docs))
	for name := range in.Docs {
		b, readErr := s.store.ReadKnowledgeDoc(ctx, in.Project, in.Repo, name)
		switch {
		case readErr == nil:
			priorBytes[name] = b
		case errors.Is(readErr, storage.ErrKnowledgeDocNotFound):
			priorBytes[name] = nil
		default:
			return nil, fmt.Errorf("snapshot prior doc %q: %w", name, readErr)
		}
	}

	// priorMetaRepo is a deep copy: struct fields are copied by value, but the
	// inner Docs map is a reference so we clone it explicitly.
	priorMetaRepo := repoMeta
	priorMetaRepo.Docs = maps.Clone(repoMeta.Docs)

	// Write each doc. Track successful writes so a mid-loop failure can
	// restore the disk to pre-call state — without this, a partial-write
	// failure that returns before the commit-failure rollback block would
	// leave some docs written-but-not-committed, diverging from git.
	humanEdited := in.Source == KnowledgeWriteSourceEdit
	now := s.clk.Now().UTC()
	written := make([]string, 0, len(in.Docs))

	rollbackWrittenDocs := func() {
		for _, name := range written {
			prior := priorBytes[name]
			if prior == nil {
				if delErr := s.store.DeleteKnowledgeDoc(ctx, in.Project, in.Repo, name); delErr != nil {
					slog.Error("knowledge mid-loop rollback: delete failed", "doc", name, "err", delErr)
				}

				continue
			}

			if writeErr := s.store.WriteKnowledgeDoc(ctx, in.Project, in.Repo, name, prior); writeErr != nil {
				slog.Error("knowledge mid-loop rollback: overwrite failed", "doc", name, "err", writeErr)
			}
		}
	}

	for name, content := range in.Docs {
		if err := s.store.WriteKnowledgeDoc(ctx, in.Project, in.Repo, name, []byte(content)); err != nil {
			// A previous iteration may already have landed; roll those back
			// before returning so caller sees the disk as it was on entry.
			rollbackWrittenDocs()

			return nil, err
		}

		repoMeta.Docs[name] = board.KnowledgeDocMeta{
			LastBuiltCommit: in.HeadCommit,
			HumanEdited:     humanEdited,
		}
		written = append(written, name)
	}

	if in.Source == KnowledgeWriteSourceRefresh {
		repoMeta.LastBuiltAt = now
		repoMeta.LastBuiltCommit = in.HeadCommit
		repoMeta.LastBuiltBy = in.AgentID
	}

	meta.SchemaVersion = 1
	meta.Repos[in.Repo] = repoMeta

	if err := s.store.WriteKnowledgeMeta(ctx, in.Project, meta); err != nil {
		// Docs were written successfully but meta write failed; restore docs
		// to their pre-call bytes so disk is not left half-updated.
		rollbackWrittenDocs()

		return nil, err
	}

	// Build commit paths and message.
	sort.Strings(written)

	paths := make([]string, 0, len(written)+1)
	for _, d := range written {
		paths = append(paths, fmt.Sprintf("%s/knowledge/%s/%s", in.Project, in.Repo, d))
	}

	paths = append(paths, fmt.Sprintf("%s/knowledge/.meta.yaml", in.Project))

	var msg string

	switch in.Source {
	case KnowledgeWriteSourceRefresh:
		msg = fmt.Sprintf("chore(knowledge): refresh %s/%s %s", in.Project, in.Repo, strings.Join(written, ", "))
	case KnowledgeWriteSourceEdit:
		msg = fmt.Sprintf("docs(knowledge): edit %s/%s %s", in.Project, in.Repo, strings.Join(written, ", "))
	}

	if err := s.knowledgeCommitFn(ctx, paths, msg); err != nil {
		// Restore disk state to what it was before this call.
		for name, b := range priorBytes {
			if b == nil {
				if delErr := s.store.DeleteKnowledgeDoc(ctx, in.Project, in.Repo, name); delErr != nil {
					slog.Error("knowledge rollback: delete failed", "doc", name, "err", delErr)
				}

				continue
			}

			if writeErr := s.store.WriteKnowledgeDoc(ctx, in.Project, in.Repo, name, b); writeErr != nil {
				slog.Error("knowledge rollback: overwrite failed", "doc", name, "err", writeErr)
			}
		}

		meta.Repos[in.Repo] = priorMetaRepo
		if writeErr := s.store.WriteKnowledgeMeta(ctx, in.Project, meta); writeErr != nil {
			slog.Error("knowledge rollback: meta write failed", "err", writeErr)
		}

		return nil, fmt.Errorf("commit knowledge docs: %w", err)
	}

	s.notifyCommit()

	// Notify the in-flight refresh registry on successful Refresh writes.
	// Edit-source writes never touch the registry. Missing-job is a no-op
	// (local-mode case where no UI-side acquire happened). Pass empty
	// commit_sha — the boards-repo SHA is not currently surfaced by
	// knowledgeCommitFn; runner-side callback (Task 13) reports its own
	// commit_sha to the UI.
	if in.Source == KnowledgeWriteSourceRefresh && s.refreshRegistry != nil {
		if err := s.refreshRegistry.MarkCommitted(in.Project, in.Repo, ""); err != nil {
			// ErrJobNotFound is the expected local-mode case: the caller
			// wrote docs without first acquiring a registry job (no runner
			// side-channel). Swallow it. Any other error is unexpected —
			// log but do not fail the write; the docs already landed.
			if !errors.Is(err, refresh.ErrJobNotFound) {
				slog.Warn("registry.MarkCommitted failed",
					"project", in.Project, "repo", in.Repo, "error", err)
			}
		}
	}

	return &WriteKnowledgeDocsResult{FilesWritten: written}, nil
}

// KnowledgeBaseRead is returned by ReadKnowledgeBase.
type KnowledgeBaseRead struct {
	Project   string                  `json:"project"`
	Repo      string                  `json:"repo"`
	Docs      map[string]string       `json:"docs"`
	Summaries map[string]string       `json:"summaries"`
	Meta      board.KnowledgeRepoMeta `json:"meta"`
}

// extractSummary returns the text content of the first ## Summary section in
// the given markdown content. It scans for the first line that is exactly
// "## Summary", collects all following lines until the next ##-level heading
// or EOF, then returns the trimmed result. Returns an empty string if no
// ## Summary section is found.
func extractSummary(content string) string {
	lines := strings.Split(content, "\n")

	inSummary := false

	var summaryLines []string

	for _, line := range lines {
		if !inSummary {
			if line == "## Summary" {
				inSummary = true
			}

			continue
		}

		// Stop at the next ##-level heading.
		if strings.HasPrefix(line, "## ") {
			break
		}

		summaryLines = append(summaryLines, line)
	}

	return strings.TrimSpace(strings.Join(summaryLines, "\n"))
}

// KnowledgeDocRead is returned by ReadKnowledgeDoc.
type KnowledgeDocRead struct {
	Content string                 `json:"content"`
	Meta    board.KnowledgeDocMeta `json:"meta"`
}

// KnowledgeBaseSummary is an entry in ListKnowledgeBases output.
type KnowledgeBaseSummary struct {
	Project string                 `json:"project"`
	Repos   []KnowledgeRepoSummary `json:"repos"`
}

// KnowledgeRepoSummary is one repo's summary within a KnowledgeBaseSummary.
// LastBuiltAt is a pointer so unbuilt-but-configured repos can omit the field
// rather than emitting Go's zero-time sentinel ("0001-01-01T00:00:00Z"), which
// the frontend would otherwise format as a centuries-old "built" time.
type KnowledgeRepoSummary struct {
	Name            string                `json:"name"`
	LastBuiltAt     *time.Time            `json:"last_built_at,omitempty"`
	LastBuiltCommit string                `json:"last_built_commit"`
	Docs            []KnowledgeDocSummary `json:"docs"`
}

// KnowledgeDocSummary is a single doc entry in a KnowledgeRepoSummary.
type KnowledgeDocSummary struct {
	Name        string `json:"name"`
	HumanEdited bool   `json:"human_edited"`
}

// resolveRepo picks the repo to use when the caller didn't specify one.
// Returns requested if non-empty. Otherwise returns the primary repo's name,
// or the only repo's name (single-repo project), or an error if the project
// has multiple repos and none is marked primary.
func (s *CardService) resolveRepo(ctx context.Context, project, requested string) (string, error) {
	if requested != "" {
		return requested, nil
	}

	cfg, err := s.store.GetProject(ctx, project)
	if err != nil {
		return "", err
	}

	repos := cfg.EffectiveRepos()
	if len(repos) == 0 {
		return "", errNoReposConfigured
	}

	if len(repos) == 1 {
		return repos[0].Name, nil
	}

	for _, r := range repos {
		if r.Primary {
			return r.Name, nil
		}
	}

	// Should be unreachable: EffectiveRepos auto-promotes the first repo
	// to primary when none is marked. Defensive guard for stale or
	// test-injected configs.
	return "", fmt.Errorf("project %s has multiple repos but none marked primary; specify repo explicitly", project)
}

// ReadKnowledgeBase returns all canonical KB docs for a project's repo,
// plus the per-repo metadata block. If the project has no repos configured
// or no KB built yet, returns an empty result (not an error).
func (s *CardService) ReadKnowledgeBase(ctx context.Context, project, repo string) (*KnowledgeBaseRead, error) {
	resolvedRepo, err := s.resolveRepo(ctx, project, repo)
	if err != nil {
		// "No repos configured" is a valid post-Jira-import state:
		// return an empty result so the UI shows its empty-state hint.
		// All other errors (project not found, multi-repo with no
		// primary, store I/O) must propagate so callers see the real
		// failure.
		if errors.Is(err, errNoReposConfigured) {
			return &KnowledgeBaseRead{Project: project, Docs: map[string]string{}, Summaries: map[string]string{}}, nil
		}

		return nil, err
	}

	meta, err := s.store.ReadKnowledgeMeta(ctx, project)
	if err != nil {
		return nil, err
	}

	docs := map[string]string{}
	summaries := map[string]string{}

	for _, name := range board.KnowledgeDocNames {
		exists, err := s.store.KnowledgeDocExists(ctx, project, resolvedRepo, name)
		if err != nil {
			return nil, err
		}

		if !exists {
			continue
		}

		data, err := s.store.ReadKnowledgeDoc(ctx, project, resolvedRepo, name)
		if err != nil {
			return nil, err
		}

		docs[name] = string(data)
		summaries[name] = extractSummary(string(data))
	}

	return &KnowledgeBaseRead{
		Project:   project,
		Repo:      resolvedRepo,
		Docs:      docs,
		Summaries: summaries,
		Meta:      meta.Repos[resolvedRepo],
	}, nil
}

// ReadKnowledgeDoc returns the content and metadata for a single doc.
func (s *CardService) ReadKnowledgeDoc(ctx context.Context, project, repo, doc string) (*KnowledgeDocRead, error) {
	resolvedRepo, err := s.resolveRepo(ctx, project, repo)
	if err != nil {
		return nil, err
	}

	data, err := s.store.ReadKnowledgeDoc(ctx, project, resolvedRepo, doc)
	if err != nil {
		return nil, err
	}

	meta, err := s.store.ReadKnowledgeMeta(ctx, project)
	if err != nil {
		return nil, err
	}

	return &KnowledgeDocRead{
		Content: string(data),
		Meta:    meta.Repos[resolvedRepo].Docs[doc],
	}, nil
}

// RefreshPlan describes which docs would be rebuilt by a refresh run,
// with reason and cost estimates per item.
type RefreshPlan struct {
	Items []RefreshPlanItem `json:"items"`
}

type RefreshPlanItem struct {
	Repo             string  `json:"repo"`
	Doc              string  `json:"doc"`
	Reason           string  `json:"reason"` // "missing" | "scheduled rebuild"
	HumanEdited      bool    `json:"human_edited"`
	EstimatedCostUSD float64 `json:"estimated_cost_usd"`
	EstimatedTokens  int     `json:"estimated_tokens"`
}

// docCostEstimates is the v1 hardcoded cost table; refined later when
// we have real telemetry from refresh runs.
var docCostEstimates = map[string]struct {
	cost   float64
	tokens int
}{
	"architecture.md":      {cost: 0.50, tokens: 25_000},
	"code-structure.md":    {cost: 1.00, tokens: 50_000},
	"api-documentation.md": {cost: 0.30, tokens: 15_000},
	"glossary.md":          {cost: 0.20, tokens: 10_000},
}

// BuildRefreshPlan computes which KB docs would be rebuilt for a refresh
// of the given project (and optional repo filter). It does not run
// sub-agents — it just inspects current state and produces a plan.
//
// Default scoping: when repoFilter is empty, single-repo projects pick the
// only repo; multi-repo projects default to primary. To rebuild a
// non-primary repo, pass repoFilter explicitly.
func (s *CardService) BuildRefreshPlan(ctx context.Context, project, repoFilter string) (*RefreshPlan, error) {
	cfg, err := s.store.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}

	repos := cfg.EffectiveRepos()
	if len(repos) == 0 {
		return &RefreshPlan{}, nil
	}

	var targets []board.Repo

	switch {
	case repoFilter != "":
		for _, r := range repos {
			if r.Name == repoFilter {
				targets = []board.Repo{r}

				break
			}
		}

		if len(targets) == 0 {
			return nil, fmt.Errorf("repo %q not found in project %s", repoFilter, project)
		}
	case len(repos) == 1:
		targets = repos
	default:
		for _, r := range repos {
			if r.Primary {
				targets = []board.Repo{r}

				break
			}
		}

		// EffectiveRepos auto-promotes the first entry to primary when
		// none is marked, so this should be unreachable. Guard anyway
		// in case a stale or test-injected config slips through.
		if len(targets) == 0 {
			return nil, fmt.Errorf("project %s has multiple repos but none marked primary; specify repo explicitly", project)
		}
	}

	meta, err := s.store.ReadKnowledgeMeta(ctx, project)
	if err != nil {
		return nil, err
	}

	plan := &RefreshPlan{}

	for _, r := range targets {
		repoMeta := meta.Repos[r.Name]
		for _, doc := range board.KnowledgeDocNames {
			docMeta, hasDoc := repoMeta.Docs[doc]

			reason := "scheduled rebuild"
			if !hasDoc {
				reason = "missing"
			}

			est := docCostEstimates[doc]
			plan.Items = append(plan.Items, RefreshPlanItem{
				Repo:             r.Name,
				Doc:              doc,
				Reason:           reason,
				HumanEdited:      docMeta.HumanEdited,
				EstimatedCostUSD: est.cost,
				EstimatedTokens:  est.tokens,
			})
		}
	}

	return plan, nil
}

// ListKnowledgeBases enumerates KB summaries across all projects (or one if
// projectFilter is set). Projects without any KB content are omitted.
func (s *CardService) ListKnowledgeBases(ctx context.Context, projectFilter string) ([]KnowledgeBaseSummary, error) {
	projects, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]KnowledgeBaseSummary, 0, len(projects))
	for _, p := range projects {
		if projectFilter != "" && p.Name != projectFilter {
			continue
		}

		meta, err := s.store.ReadKnowledgeMeta(ctx, p.Name)
		if err != nil {
			return nil, err
		}

		if len(meta.Repos) == 0 {
			continue
		}

		summary := KnowledgeBaseSummary{Project: p.Name}

		for repoName, repoMeta := range meta.Repos {
			var lastBuiltAt *time.Time

			if !repoMeta.LastBuiltAt.IsZero() {
				t := repoMeta.LastBuiltAt
				lastBuiltAt = &t
			}

			rs := KnowledgeRepoSummary{
				Name:            repoName,
				LastBuiltAt:     lastBuiltAt,
				LastBuiltCommit: repoMeta.LastBuiltCommit,
			}
			for docName, docMeta := range repoMeta.Docs {
				rs.Docs = append(rs.Docs, KnowledgeDocSummary{Name: docName, HumanEdited: docMeta.HumanEdited})
			}

			sort.Slice(rs.Docs, func(i, j int) bool { return rs.Docs[i].Name < rs.Docs[j].Name })
			summary.Repos = append(summary.Repos, rs)
		}

		sort.Slice(summary.Repos, func(i, j int) bool { return summary.Repos[i].Name < summary.Repos[j].Name })
		out = append(out, summary)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Project < out[j].Project })

	return out, nil
}
