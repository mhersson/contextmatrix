package boardmerge

import (
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

// resolvePlaybook merges one playbooks/<id>.yaml three-way. Entries merge by
// entry id as three-way records; next_entry_id takes the max of both sides.
// The same entry id added independently on both sides with different content
// keeps theirs and re-mints ours under a fresh id. title and description are
// plain three-way scalars with the later-updated side breaking a real
// conflict; updated_at is the later of the two. id, created_by and created_at
// are immutable after creation and carried forward unchanged. Two instances
// creating a playbook at the same path with no common ancestor re-slugs the
// local copy to a free id instead of colliding.
func resolvePlaybook(in Input, id string, c Context) (Output, error) {
	if in.Ours == nil || in.Theirs == nil {
		if kept, survivor, stopped := activeRunSurvivor(in); kept != nil {
			detail := "deleted on one side while its run is active on the other; kept"

			if stopped {
				// The deleter saw base's active run and a local delete refuses
				// one, so it stopped that run first.
				data, err := stopSurvivorRun(survivor, c.Now())
				if err != nil {
					return Output{}, fmt.Errorf("stop surviving playbook run %s: %w", id, err)
				}

				kept = data
				detail = "deleted on one side after stopping the run the other side still had active; kept with the run stopped"
			}

			return Output{Content: kept, Resolutions: []Resolution{{
				Path: in.Path, Rule: RulePlaybookRunOverDelete, Detail: detail,
			}}}, nil
		}

		return Output{Deleted: true, Resolutions: []Resolution{{
			Path: in.Path, Rule: RuleDeleteWins, Detail: "playbook deleted on one side",
		}}}, nil
	}

	ours, oursErr := board.ParsePlaybook(in.Ours)
	theirs, theirsErr := board.ParsePlaybook(in.Theirs)

	switch {
	case oursErr != nil && theirsErr != nil:
		return Output{Content: in.Theirs, Resolutions: []Resolution{{
			Path: in.Path, Rule: RuleUnparseable, Detail: "neither side parses; remote kept: " + theirsErr.Error(),
		}}}, nil
	case oursErr != nil:
		return Output{Content: in.Theirs, Resolutions: []Resolution{{
			Path: in.Path, Rule: RuleUnparseable, Detail: "local side unparseable: " + oursErr.Error(),
		}}}, nil
	case theirsErr != nil:
		return Output{Content: in.Ours, Resolutions: []Resolution{{
			Path: in.Path, Rule: RuleUnparseable, Detail: "remote side unparseable: " + theirsErr.Error(),
		}}}, nil
	}

	if in.Base == nil {
		return resolvePlaybookAddAdd(in.Path, id, ours, in.Theirs, c)
	}

	base := &board.Playbook{}
	if b, err := board.ParsePlaybook(in.Base); err == nil {
		base = b
	}

	out, res := mergePlaybooks(base, ours, theirs, in.Path, c)

	// The index skips a playbook that fails Validate, hiding it on every
	// instance; keep the remote version rather than write one.
	if err := out.Validate(); err != nil {
		return Output{Content: in.Theirs, Resolutions: append(res, Resolution{
			Path: in.Path, Rule: RuleInvariantFallback, Detail: "merged playbook invalid (" + err.Error() + "); remote version kept",
		})}, nil
	}

	data, err := board.SerializePlaybook(out)
	if err != nil {
		return Output{}, fmt.Errorf("serialize merged playbook %s: %w", id, err)
	}

	return Output{Content: data, Resolutions: res}, nil
}

// resolvePlaybookAddAdd handles two instances independently creating a
// playbook at the same path. The remote keeps the id; the local copy is
// re-slugged to the first free "<id>-N" and written alongside it.
func resolvePlaybookAddAdd(path, id string, ours *board.Playbook, theirsContent []byte, c Context) (Output, error) {
	newID := nextPlaybookSlug(id, c.PlaybookExists)

	local := *ours
	local.ID = newID

	data, err := board.SerializePlaybook(&local)
	if err != nil {
		return Output{}, fmt.Errorf("serialize re-slugged playbook %s: %w", newID, err)
	}

	return Output{
		Content: theirsContent,
		Extra:   []File{{Path: "playbooks/" + newID + ".yaml", Content: data}},
		Resolutions: []Resolution{{
			Path: path, Rule: RulePlaybookReslug, OldID: id, NewID: newID,
			Detail: "remote kept the id, local playbook re-slugged",
		}},
	}, nil
}

// nextPlaybookSlug returns the first "<id>-N" (N >= 2) that exists reports as
// free.
func nextPlaybookSlug(id string, exists func(string) bool) string {
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", id, n)
		if exists == nil || !exists(candidate) {
			return candidate
		}
	}
}

// mergePlaybooks merges base/ours/theirs into a new Playbook. It never
// mutates its inputs and never aliases their slices.
func mergePlaybooks(base, ours, theirs *board.Playbook, path string, c Context) (*board.Playbook, []Resolution) {
	var res []Resolution

	out := *theirs // start from remote; id/created_by/created_at stay theirs (immutable, identical on both sides)

	later := theirs
	if ours.Updated.After(theirs.Updated) {
		later = ours
	}

	conflict := func(field string) {
		res = append(res, Resolution{Path: path, Rule: RuleLaterUpdated, Detail: field + ": both sides changed; later updated_at kept"})
	}

	if v, cf := pick(base.Title, ours.Title, theirs.Title); cf {
		out.Title = later.Title

		conflict("title")
	} else {
		out.Title = v
	}

	if v, cf := pick(base.Description, ours.Description, theirs.Description); cf {
		out.Description = later.Description

		conflict("description")
	} else {
		out.Description = v
	}

	// Runnable, base_branch and the run block follow the title rule: a
	// one-sided change wins, both-sided changes go to the later side. The
	// run block is compared as a unit so a stale peer cannot splice fields
	// of two different runs together.
	if v, cf := pick(base.Runnable, ours.Runnable, theirs.Runnable); cf {
		out.Runnable = later.Runnable

		conflict("runnable")
	} else {
		out.Runnable = v
	}

	if v, cf := pick(base.BaseBranch, ours.BaseBranch, theirs.BaseBranch); cf {
		out.BaseBranch = later.BaseBranch

		conflict("base_branch")
	} else {
		out.BaseBranch = v
	}

	if v, cf := pickEq(base.Run, ours.Run, theirs.Run, func(a, b *board.PlaybookRun) bool { return reflect.DeepEqual(a, b) }); cf {
		out.Run = later.Run

		conflict("run")
	} else {
		out.Run = v
	}

	// Turning runnable off refuses an active run, so a side that did it never
	// saw the other side's run unless it is the run base held: keep that run
	// runnable.
	if !out.Runnable {
		for _, p := range []*board.Playbook{ours, theirs} {
			if p.Runnable && p.RunActive() && (!base.RunActive() || !sameRun(base.Run, p.Run)) {
				out.Runnable = true
				out.Run = p.Run.Clone()

				res = append(res, Resolution{
					Path: path, Rule: RulePlaybookRunKeepsRunnable,
					Detail: "runnable turned off on one side while the other side's run is active; kept runnable",
				})

				break
			}
		}
	}

	// A run block only makes sense on a runnable playbook; Validate rejects
	// the combination, so repair it here rather than leave the file unusable.
	if !out.Runnable {
		out.Run = nil
	}

	out.NextEntryID = max(ours.NextEntryID, theirs.NextEntryID)
	out.Updated = later.Updated

	entries, entryRes := mergePlaybookEntries(base.Entries, ours.Entries, theirs.Entries, &out, path, later == ours, c.Renames)
	out.Entries = entries

	res = append(res, entryRes...)
	res = append(res, keepRunEntry(&out, base, ours, theirs, path, c.Renames)...)
	res = append(res, clearDanglingRunEntry(&out, path)...)

	return &out, res
}

func mergePlaybookEntries(
	base, ours, theirs []board.PlaybookEntry, out *board.Playbook, path string, oursLater bool, renames map[string]string,
) ([]board.PlaybookEntry, []Resolution) {
	var res []Resolution

	byID := func(es []board.PlaybookEntry) map[string]board.PlaybookEntry {
		m := make(map[string]board.PlaybookEntry, len(es))
		for _, e := range es {
			m[e.ID] = e
		}

		return m
	}

	bm, om, tm := byID(base), byID(ours), byID(theirs)

	var merged []board.PlaybookEntry

	for _, te := range theirs {
		oe, inOurs := om[te.ID]
		be, inBase := bm[te.ID]

		switch {
		case !inOurs && inBase:
			continue // ours removed it
		case !inOurs:
			merged = append(merged, te) // theirs added it
		case reflect.DeepEqual(oe, te):
			merged = append(merged, te)
		case inBase && reflect.DeepEqual(oe, be):
			merged = append(merged, te) // only theirs changed it
		case inBase && reflect.DeepEqual(te, be):
			merged = append(merged, oe) // only ours changed it
		case !inBase:
			// same id added on both sides with different content: keep theirs,
			// re-mint ours under a fresh id.
			merged = append(merged, te)

			re := oe
			re.ID = fmt.Sprintf("e%d", out.NextEntryID)
			out.NextEntryID++

			merged = append(merged, applyEntryRename(re, renames))

			res = append(res, Resolution{Path: path, Rule: RulePlaybookRemint, OldID: oe.ID, NewID: re.ID})
		default:
			res = append(res, Resolution{
				Path: path, Rule: RuleLaterUpdated,
				Detail: "entry " + te.ID + ": both sides changed; later updated_at kept",
			})

			if oursLater {
				merged = append(merged, oe)
			} else {
				merged = append(merged, te)
			}
		}
	}

	for _, oe := range ours {
		if _, inTheirs := tm[oe.ID]; inTheirs {
			continue
		}

		if _, inBase := bm[oe.ID]; inBase {
			continue // theirs removed it
		}

		merged = append(merged, applyEntryRename(oe, renames))
	}

	deduped := make([]board.PlaybookEntry, 0, len(merged))
	seen := map[string]bool{}

	for _, e := range merged {
		key := e.Type + "|" + e.Project + "|" + e.Card
		if e.Type == board.EntryTypeCard && seen[key] {
			res = append(res, Resolution{
				Path: path, Rule: RulePlaybookDedupe,
				Detail: "duplicate entry for " + e.Project + "/" + e.Card + " dropped",
			})

			continue
		}

		seen[key] = true

		deduped = append(deduped, e)
	}

	return deduped, res
}

func applyEntryRename(e board.PlaybookEntry, renames map[string]string) board.PlaybookEntry {
	if e.Card == "" {
		return e
	}

	if n, ok := renames[e.Project+"/"+e.Card]; ok {
		e.Card = n
	}

	return e
}

// besideBaseNeighbour is where base entry i goes back into out: after its
// nearest earlier base entry out kept, else before its nearest later one,
// else at the end.
func besideBaseNeighbour(base []board.PlaybookEntry, i int, out *board.Playbook) int {
	for j := i - 1; j >= 0; j-- {
		if k := out.FindEntry(base[j].ID); k >= 0 {
			return k + 1
		}
	}

	for j := i + 1; j < len(base); j++ {
		if k := out.FindEntry(base[j].ID); k >= 0 {
			return k
		}
	}

	return len(out.Entries)
}

// keepRunEntry carries "the run's current entry stays until the run stops"
// through a merge: a stale peer's removal is undone beside the entry's
// nearest surviving base neighbour, and a dedupe re-points the run to the surviving entry for its card.
func keepRunEntry(out, base, ours, theirs *board.Playbook, path string, renames map[string]string) []Resolution {
	if !out.Run.Active() || out.Run.Entry == "" || out.FindEntry(out.Run.Entry) >= 0 {
		return nil
	}

	id := out.Run.Entry

	lost, ok := entryByID(theirs, id)
	if !ok {
		// Only an entry ours added since base can reference a card ours re-minted.
		if lost, ok = entryByID(ours, id); ok && base.FindEntry(id) < 0 {
			lost = applyEntryRename(lost, renames)
		}
	}

	if !ok {
		lost, ok = entryByID(base, id)
	}

	if !ok {
		return nil
	}

	if lost.Type == board.EntryTypeCard {
		for _, e := range out.Entries {
			if e.Type == board.EntryTypeCard && e.Project == lost.Project && e.Card == lost.Card {
				out.Run = out.Run.Clone()
				out.Run.Entry = e.ID

				return []Resolution{{
					Path: path, Rule: RulePlaybookRunEntry,
					Detail: "run entry " + id + " re-pointed to " + e.ID + ", the surviving entry for " + e.Project + "/" + e.Card,
				}}
			}
		}
	}

	at := len(out.Entries)
	if i := base.FindEntry(id); i >= 0 {
		at = besideBaseNeighbour(base.Entries, i, out)
	}

	out.Entries = slices.Insert(out.Entries, at, lost)

	return []Resolution{{Path: path, Rule: RulePlaybookRunEntry, Detail: "entry " + id + " kept: it is the active run's current entry"}}
}

// clearDanglingRunEntry clears a run entry that names no entry, with its
// reason, as RemoveEntry does; an active run's entry was restored above.
func clearDanglingRunEntry(out *board.Playbook, path string) []Resolution {
	if out.Run == nil || out.Run.Entry == "" || out.FindEntry(out.Run.Entry) >= 0 {
		return nil
	}

	id := out.Run.Entry
	out.Run = out.Run.Clone()
	out.Run.Entry = ""
	out.Run.Reason = ""

	return []Resolution{{Path: path, Rule: RuleInvariantRepair, Detail: "run entry " + id + " no longer exists; cleared"}}
}

// entryByID returns p's entry with id, if it has one.
func entryByID(p *board.Playbook, id string) (board.PlaybookEntry, bool) {
	if i := p.FindEntry(id); i >= 0 {
		return p.Entries[i], true
	}

	return board.PlaybookEntry{}, false
}

// activeRunSurvivor returns the surviving side of a delete/modify conflict
// when it holds an active run; a local delete refuses one, so the deleter
// was stale. stopped reports that the survivor's run is the active run base
// held, so the deleter had stopped it.
func activeRunSurvivor(in Input) (kept []byte, parsed *board.Playbook, stopped bool) {
	survivor := in.Ours
	if survivor == nil {
		survivor = in.Theirs
	}

	if survivor == nil {
		return nil, nil, false
	}

	p, err := board.ParsePlaybook(survivor)
	if err != nil || !p.Runnable || !p.RunActive() {
		return nil, nil, false
	}

	if in.Base != nil {
		if b, err := board.ParsePlaybook(in.Base); err == nil && b.RunActive() && sameRun(b.Run, p.Run) {
			return survivor, p, true
		}
	}

	return survivor, p, false
}

// stopSurvivorRun returns p serialized with its run stopped at now.
func stopSurvivorRun(p *board.Playbook, now time.Time) ([]byte, error) {
	p.Run = p.Run.Clone()
	p.Run.Status = board.RunStatusStopped
	p.Run.Reason = "stopped and deleted on another instance"
	p.Run.UpdatedAt = now
	p.Run.EndedAt = &now

	data, err := board.SerializePlaybook(p)
	if err != nil {
		return nil, fmt.Errorf("serialize playbook: %w", err)
	}

	return data, nil
}

// sameRun reports whether a and b are the same run: one instance's start.
func sameRun(a, b *board.PlaybookRun) bool {
	return a != nil && b != nil && a.Instance == b.Instance && a.StartedAt.Equal(b.StartedAt)
}
