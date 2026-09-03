package boardmerge

import (
	"fmt"
	"reflect"

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

	out.NextEntryID = max(ours.NextEntryID, theirs.NextEntryID)
	out.Updated = later.Updated

	entries, entryRes := mergePlaybookEntries(base.Entries, ours.Entries, theirs.Entries, &out, path, later == ours, c.Renames)
	out.Entries = entries

	res = append(res, entryRes...)

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
