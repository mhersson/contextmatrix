package gitsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/boardmerge"
	"github.com/mhersson/contextmatrix/internal/gitops"
)

// resolveConflicts resolves every unmerged path a conflicted merge left behind
// and stages the result, so the caller can conclude the merge with a commit.
//
// Paths resolve in dependency order: project configs first, so a re-mint draws
// from the merged next_id; then the cards both sides added, because those are
// what produce renames; then the remaining cards and the playbooks, which
// follow those renames; then everything else. When any card was re-minted, the
// references our side added since the fork point are rewritten last.
//
// A failure leaves nothing of its own behind: the files written for re-minted
// cards are removed before returning and the caller aborts the merge.
func (s *Syncer) resolveConflicts(
	ctx context.Context, branch string, oursChanged []string,
) ([]boardmerge.Resolution, error) {
	unmerged, err := s.git.UnmergedPaths(ctx)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(unmerged, func(i, j int) bool { return resolveRank(unmerged[i]) < resolveRank(unmerged[j]) })

	mctx, err := s.mergeContext(ctx, branch)
	if err != nil {
		return nil, err
	}

	s.extraWritten = nil

	renames := map[string]string{}
	conflicted := map[string]bool{}

	var all []boardmerge.Resolution

	for _, u := range unmerged {
		conflicted[u.Path] = true

		in := boardmerge.Input{Path: u.Path}

		for stage, dst := range map[int]*[]byte{1: &in.Base, 2: &in.Ours, 3: &in.Theirs} {
			data, stageErr := s.git.ShowStage(ctx, stage, u.Path)
			if stageErr != nil {
				s.removeExtras()

				return nil, stageErr
			}

			*dst = data
		}

		// Renames carries every re-mint made so far this merge, so a card or
		// playbook resolved later follows the ids the earlier ones handed out.
		mctx.Renames = renames

		out, resErr := boardmerge.Resolve(in, mctx)
		if resErr != nil {
			s.removeExtras()

			return nil, fmt.Errorf("resolve %s: %w", u.Path, resErr)
		}

		if err := s.applyOutput(ctx, u.Path, out); err != nil {
			s.removeExtras()

			return nil, err
		}

		maps.Copy(renames, out.Renames)

		all = append(all, out.Resolutions...)
	}

	if len(renames) > 0 {
		if err := s.rewriteLocalRefs(ctx, renames, oursChanged, conflicted); err != nil {
			s.removeExtras()

			return nil, err
		}
	}

	return all, nil
}

// applyOutput writes and stages what the resolver produced for one path.
func (s *Syncer) applyOutput(ctx context.Context, path string, out boardmerge.Output) error {
	if out.Deleted {
		if err := s.git.RemovePaths(ctx, []string{path}); err != nil {
			return err
		}
	} else if err := s.writeAndStage(ctx, path, out.Content); err != nil {
		return err
	}

	for _, f := range out.Extra {
		// Recorded before the write, so a file that lands but fails to stage
		// is still cleaned up. Only a path we create is recorded: a path that
		// already exists is one the merge abort restores, and deleting that
		// would dirty the tree the cleanup is meant to leave clean.
		if _, err := os.Stat(filepath.Join(s.repoPath, f.Path)); errors.Is(err, os.ErrNotExist) {
			s.extraWritten = append(s.extraWritten, f.Path)
		}

		if err := s.writeAndStage(ctx, f.Path, f.Content); err != nil {
			return err
		}
	}

	return nil
}

// removeExtras deletes the files written for re-minted cards and forgets them.
// Called on every path that gives up on the merge, so the next cycle does not
// find them as untracked leftovers, commit them as an external edit, and
// re-mint the same card a second time.
func (s *Syncer) removeExtras() {
	for _, p := range s.extraWritten {
		if err := os.Remove(filepath.Join(s.repoPath, p)); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("git sync: could not remove re-minted file after a failed merge", "path", p, "error", err)
		}
	}

	s.extraWritten = nil
}

// resolveRank orders the unmerged paths so each kind sees the results of the
// kinds it depends on.
func resolveRank(u gitops.UnmergedPath) int {
	kind, _, _ := boardmerge.Classify(u.Path)

	switch {
	case kind == boardmerge.KindProject:
		return 0
	case kind == boardmerge.KindCard && !u.HasBase:
		return 1
	case kind == boardmerge.KindCard:
		return 2
	case kind == boardmerge.KindPlaybook:
		return 3
	default:
		return 4
	}
}

func (s *Syncer) writeAndStage(ctx context.Context, path string, content []byte) error {
	full := filepath.Join(s.repoPath, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}

	if err := os.WriteFile(full, content, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return s.git.StagePaths(ctx, []string{path})
}

// mergeContext binds the pure resolver to this worktree: the project configs
// and card files it reads are the merged ones already staged, so a re-mint
// draws its id from the merged next_id rather than either side's.
func (s *Syncer) mergeContext(ctx context.Context, branch string) (boardmerge.Context, error) {
	ours, err := s.git.RevParseShort(ctx, "HEAD")
	if err != nil {
		return boardmerge.Context{}, err
	}

	theirs, err := s.git.RevParseShort(ctx, "origin/"+branch)
	if err != nil {
		return boardmerge.Context{}, err
	}

	loadProject := func(project string) (*board.ProjectConfig, error) {
		data, err := os.ReadFile(filepath.Join(s.repoPath, project, ".board.yaml"))
		if err != nil {
			return nil, fmt.Errorf("read project config: %w", err)
		}

		return board.ParseProjectConfig(data)
	}

	return boardmerge.Context{
		Instance:     s.instance,
		OursCommit:   ours,
		TheirsCommit: theirs,
		Now:          s.clk.Now,
		MergeBody: func(base, o, t string) (string, bool) {
			merged, clean, err := gitops.MergeFileText(ctx, base, o, t)
			if err != nil {
				slog.Warn("git sync: merge-file failed, falling back to later updated", "error", err)

				return "", false
			}

			return merged, clean
		},
		Project: loadProject,
		MintID: func(project string) (string, error) {
			cfg, err := loadProject(project)
			if err != nil {
				return "", err
			}

			id := board.GenerateCardID(cfg)
			if err := board.SaveProjectConfig(filepath.Join(s.repoPath, project), cfg); err != nil {
				return "", fmt.Errorf("save project config after mint: %w", err)
			}

			if err := s.git.StagePaths(ctx, []string{project + "/.board.yaml"}); err != nil {
				return "", err
			}

			return id, nil
		},
		CardExists: func(project, id string) bool {
			_, err := os.Stat(filepath.Join(s.repoPath, project, "tasks", id+".md"))

			return err == nil
		},
		PlaybookExists: func(id string) bool {
			_, err := os.Stat(filepath.Join(s.repoPath, "playbooks", id+".yaml"))

			return err == nil
		},
	}, nil
}

// rewriteLocalRefs maps references to re-minted ids in the files only our side
// changed since the merge base. A reference to an old id in such a file was
// added locally, so it means the local card. Files both sides touched were
// conflicted and the resolver already followed the renames for them; a file
// only the remote changed refers to the remote card and must stay.
func (s *Syncer) rewriteLocalRefs(
	ctx context.Context, renames map[string]string, oursChanged []string, conflicted map[string]bool,
) error {
	for _, path := range oursChanged {
		if conflicted[path] {
			continue
		}

		kind, project, _ := boardmerge.Classify(path)
		if kind != boardmerge.KindCard && kind != boardmerge.KindPlaybook {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.repoPath, path))
		if err != nil {
			continue // deleted by the merge
		}

		out, err := rewriteRefs(kind, project, data, renames)
		if err != nil {
			return err
		}

		if out == nil {
			continue // nothing to rewrite, or the file does not parse
		}

		if err := s.writeAndStage(ctx, path, out); err != nil {
			return err
		}
	}

	return nil
}

// rewriteRefs applies renames to one card or playbook file, returning nil when
// it holds no reference to a re-minted id or does not parse. An unparseable
// file is left exactly as the merge produced it: the resolver owns the files
// it conflicted on, and this one is not among them.
func rewriteRefs(kind boardmerge.Kind, project string, data []byte, renames map[string]string) ([]byte, error) {
	if kind == boardmerge.KindPlaybook {
		return rewritePlaybookRefs(data, renames)
	}

	card, err := board.ParseCard(data)
	if err != nil {
		return nil, nil //nolint:nilnil // unparseable is "nothing to rewrite", not a failure
	}

	changed := false

	ren := func(id string) string {
		if n, ok := renames[project+"/"+id]; ok {
			changed = true

			return n
		}

		return id
	}

	card.Parent = ren(card.Parent)

	for i := range card.DependsOn {
		card.DependsOn[i] = ren(card.DependsOn[i])
	}

	for i := range card.Subtasks {
		card.Subtasks[i] = ren(card.Subtasks[i])
	}

	if !changed {
		return nil, nil //nolint:nilnil // no reference to a re-minted id
	}

	out, err := board.SerializeCard(card)
	if err != nil {
		return nil, fmt.Errorf("serialize %s after rewriting references: %w", card.ID, err)
	}

	return out, nil
}

func rewritePlaybookRefs(data []byte, renames map[string]string) ([]byte, error) {
	pb, err := board.ParsePlaybook(data)
	if err != nil {
		return nil, nil //nolint:nilnil // unparseable is "nothing to rewrite", not a failure
	}

	changed := false

	for i, e := range pb.Entries {
		if e.Card == "" {
			continue
		}

		if n, ok := renames[e.Project+"/"+e.Card]; ok {
			pb.Entries[i].Card = n
			changed = true
		}
	}

	if !changed {
		return nil, nil //nolint:nilnil // no reference to a re-minted id
	}

	out, err := board.SerializePlaybook(pb)
	if err != nil {
		return nil, fmt.Errorf("serialize playbook %s after rewriting references: %w", pb.ID, err)
	}

	return out, nil
}
