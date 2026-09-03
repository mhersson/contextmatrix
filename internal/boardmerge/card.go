package boardmerge

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/mhersson/contextmatrix/internal/board"
)

func resolveCard(in Input, project, id string, c Context) (Output, error) {
	if in.Ours == nil || in.Theirs == nil {
		side := "remote"
		if in.Theirs != nil {
			side = "local"
		}

		return Output{Deleted: true, Resolutions: []Resolution{{
			Path: in.Path, CardID: id, Rule: RuleDeleteWins,
			Detail: "deleted on " + side + ", modification kept in history",
		}}}, nil
	}

	ours, oursErr := board.ParseCard(in.Ours)
	theirs, theirsErr := board.ParseCard(in.Theirs)

	switch {
	case oursErr != nil && theirsErr != nil:
		return unparseableOutput(in.Path, id, in.Theirs, "neither side parses; remote kept: "+theirsErr.Error()), nil
	case oursErr != nil:
		return unparseableOutput(in.Path, id, in.Theirs, "local side unparseable: "+oursErr.Error()), nil
	case theirsErr != nil:
		return unparseableOutput(in.Path, id, in.Ours, "remote side unparseable: "+theirsErr.Error()), nil
	}

	if in.Base == nil {
		return resolveAddAdd(in, project, id, ours, theirs, c)
	}

	// An unparseable ancestor merges as if every field had changed on both
	// sides, which the field rules already handle.
	base, err := board.ParseCard(in.Base)
	if err != nil {
		base = &board.Card{}
	}

	merged, res := mergeCards(base, ours, theirs, project, c)

	merged, invRes := applyInvariants(merged, theirs, project, c)
	res = append(res, invRes...)

	if len(res) == 0 {
		// Git saw a conflict but every field merged by rule. Record it so the
		// sync log still shows the file was touched.
		res = append(res, Resolution{Path: in.Path, CardID: id, Rule: RuleFieldMerge, Detail: "no overrides"})
	}

	data, err := board.SerializeCard(merged)
	if err != nil {
		return Output{}, fmt.Errorf("serialize merged card %s: %w", id, err)
	}

	return Output{Content: data, Resolutions: res}, nil
}

func unparseableOutput(path, id string, content []byte, detail string) Output {
	return Output{Content: content, Resolutions: []Resolution{{
		Path: path, CardID: id, Rule: RuleUnparseable, Detail: detail,
	}}}
}

// resolveAddAdd handles two sides that created the same card path. The remote
// keeps the ID; the local card is re-minted unless both sides imported the
// same external issue, in which case they are the same card.
func resolveAddAdd(in Input, project, id string, ours, theirs *board.Card, c Context) (Output, error) {
	if sameImport(ours.Source, theirs.Source) {
		return Output{Content: in.Theirs, Resolutions: []Resolution{{
			Path: in.Path, CardID: id, Rule: RuleSourceDedupe,
			Detail: "both sides imported " + ours.Source.System + " id " + ours.Source.ExternalID,
		}}}, nil
	}

	if c.MintID == nil {
		return Output{}, fmt.Errorf("add/add on %s: no MintID in context", in.Path)
	}

	newID, err := c.MintID(project)
	if err != nil {
		return Output{}, fmt.Errorf("mint id for re-minted %s: %w", id, err)
	}

	local := *ours
	local.ID = newID

	local.ActivityLog = append(slices.Clone(ours.ActivityLog), auditEntry(c, RuleAddAddRemint,
		fmt.Sprintf("re-minted from %s: remote created the same id first (%s)", id, c.TheirsCommit)))
	local.ActivityLog = board.TrimActivityLog(local.ActivityLog)

	data, err := board.SerializeCard(&local)
	if err != nil {
		return Output{}, fmt.Errorf("serialize re-minted card %s: %w", newID, err)
	}

	return Output{
		Content: in.Theirs,
		Extra:   []File{{Path: project + "/tasks/" + newID + ".md", Content: data}},
		Renames: map[string]string{project + "/" + id: newID},
		Resolutions: []Resolution{{
			Path: in.Path, CardID: id, Rule: RuleAddAddRemint, OldID: id, NewID: newID,
			Detail: "remote kept the id, local card re-minted",
		}},
	}, nil
}

func sameImport(ours, theirs *board.Source) bool {
	return ours != nil && theirs != nil && ours.ExternalID != "" &&
		ours.System == theirs.System && ours.ExternalID == theirs.ExternalID
}

func auditEntry(c Context, rule, detail string) board.ActivityEntry {
	return board.ActivityEntry{
		Agent: "system", Action: board.MergeAction, Timestamp: c.Now(),
		Message: rule + ": " + detail + " (instance " + c.Instance + ")",
	}
}

// mergeCards applies the per-field rules to one card. It never returns nil and
// never mutates base, ours or theirs.
func mergeCards(base, ours, theirs *board.Card, project string, c Context) (*board.Card, []Resolution) {
	var (
		res    []Resolution
		audits []board.ActivityEntry
	)

	out := *theirs // start from remote; every field below is reassigned
	path := project + "/tasks/" + ours.ID + ".md"
	oursLater := ours.Updated.After(theirs.Updated)

	audit := func(rule, field, losing string) {
		res = append(res, Resolution{
			Path: path, CardID: ours.ID, Rule: rule,
			Detail: field + ": local (" + c.OursCommit + ") vs remote (" + c.TheirsCommit + ")",
		})
		audits = append(audits, auditEntry(c, rule, field+" from "+losing+" overridden"))
	}

	scalar := func(field string, b, o, t string) string {
		return pickLater(b, o, t, oursLater, audit, field)
	}

	// A three-way bool never conflicts: with two values, a side that differs
	// from the other always matches the ancestor. flag exists for uniformity.
	flag := func(field string, b, o, t bool) bool {
		return pickLater(b, o, t, oursLater, audit, field)
	}

	count := func(field string, b, o, t int) int {
		return pickLater(b, o, t, oursLater, audit, field)
	}

	// Immutable after creation.
	out.ID, out.Project, out.Created = ours.ID, ours.Project, ours.Created
	out.Source = firstNonNil(ours.Source, theirs.Source)

	// State: a terminal state absorbs a non-terminal one.
	switch {
	case ours.State == theirs.State:
		out.State = ours.State
	case board.IsTerminalState(ours.State) != board.IsTerminalState(theirs.State):
		winner, loserState, losing := ours.State, theirs.State, sideRemote
		if board.IsTerminalState(theirs.State) {
			winner, loserState, losing = theirs.State, ours.State, sideLocal
		}

		out.State = winner

		// The overridden side is the non-terminal one, whichever was updated
		// last. It lost something only if it actually moved: a one-sided move
		// into a terminal state overrides nothing.
		if loserState != base.State {
			audit(RuleTerminalWins, "state", losing)
		}
	default:
		out.State = scalar("state", base.State, ours.State, theirs.State)
	}

	// Scalars. The claim fields (assigned_agent, last_heartbeat, worker_status,
	// phase) are plain scalars here; the claim epoch replaces them later.
	out.Title = scalar("title", base.Title, ours.Title, theirs.Title)
	out.Type = scalar("type", base.Type, ours.Type, theirs.Type)
	out.Priority = scalar("priority", base.Priority, ours.Priority, theirs.Priority)
	out.Parent = scalar("parent", base.Parent, mapRef(ours.Parent, base.Parent, project, c.Renames), theirs.Parent)
	out.Assignee = scalar("assignee", base.Assignee, ours.Assignee, theirs.Assignee)
	out.AssignedAgent = scalar("assigned_agent", base.AssignedAgent, ours.AssignedAgent, theirs.AssignedAgent)
	out.ModelOrchestrator = scalar("model_orchestrator", base.ModelOrchestrator, ours.ModelOrchestrator, theirs.ModelOrchestrator)
	out.ModelCoder = scalar("model_coder", base.ModelCoder, ours.ModelCoder, theirs.ModelCoder)
	out.ModelReviewer = scalar("model_reviewer", base.ModelReviewer, ours.ModelReviewer, theirs.ModelReviewer)
	out.BranchName = scalar("branch_name", base.BranchName, ours.BranchName, theirs.BranchName)
	out.BaseBranch = scalar("base_branch", base.BaseBranch, ours.BaseBranch, theirs.BaseBranch)
	out.PRUrl = scalar("pr_url", base.PRUrl, ours.PRUrl, theirs.PRUrl)
	out.WorkerStatus = scalar("worker_status", base.WorkerStatus, ours.WorkerStatus, theirs.WorkerStatus)
	out.Phase = scalar("phase", base.Phase, ours.Phase, theirs.Phase)

	// Flags and counters.
	out.Autonomous = flag("autonomous", base.Autonomous, ours.Autonomous, theirs.Autonomous)
	out.MaxCapability = flag("max_capability", base.MaxCapability, ours.MaxCapability, theirs.MaxCapability)
	out.Vetted = flag("vetted", base.Vetted, ours.Vetted, theirs.Vetted)
	out.CreatePR = flag("create_pr", base.CreatePR, ours.CreatePR, theirs.CreatePR)
	out.AwaitCI = flag("await_ci", base.AwaitCI, ours.AwaitCI, theirs.AwaitCI)
	out.AwaitCopilotReview = flag("await_copilot_review", base.AwaitCopilotReview, ours.AwaitCopilotReview, theirs.AwaitCopilotReview)
	out.BestOfN = count("best_of_n", base.BestOfN, ours.BestOfN, theirs.BestOfN)
	out.MobParticipants = count("mob_participants", base.MobParticipants, ours.MobParticipants, theirs.MobParticipants)
	out.ReviewAttempts = max(ours.ReviewAttempts, theirs.ReviewAttempts)

	// Pointers and structs compared by value.
	out.LastHeartbeat = pickHeartbeat(base.LastHeartbeat, ours.LastHeartbeat, theirs.LastHeartbeat)

	verify, conflict := pickEq(base.Verify, ours.Verify, theirs.Verify, func(a, b *board.VerifyConfig) bool {
		return reflect.DeepEqual(a, b)
	})
	if conflict {
		verify = theirs.Verify
		if oursLater {
			verify = ours.Verify
		}

		audit(RuleLaterUpdated, "verify", loserSide(oursLater))
	}

	out.Verify = verify
	out.Custom = mergeCustom(base.Custom, ours.Custom, theirs.Custom, oursLater, audit)

	// Sets. Card references we add locally follow any card re-minted this sync.
	renames := projectRenames(project, c.Renames)

	out.Skills = mergeSkills(base.Skills, ours.Skills, theirs.Skills)
	out.Subtasks = mergeSet(base.Subtasks, ours.Subtasks, theirs.Subtasks, renames)
	out.DependsOn = mergeSet(base.DependsOn, ours.DependsOn, theirs.DependsOn, renames)
	out.Context = mergeSet(base.Context, ours.Context, theirs.Context, nil)
	out.Labels = mergeSet(base.Labels, ours.Labels, theirs.Labels, nil)
	out.MobPhases = mergeSet(base.MobPhases, ours.MobPhases, theirs.MobPhases, nil)
	out.MobGuests = mergeSet(base.MobGuests, ours.MobGuests, theirs.MobGuests, nil)

	// Additive.
	out.TokenUsage = mergeTokenUsage(base.TokenUsage, ours.TokenUsage, theirs.TokenUsage)
	out.UsageBreakdown = mergeBuckets(base.UsageBreakdown, ours.UsageBreakdown, theirs.UsageBreakdown)

	// Computed on read, never persisted: recomputed by whoever loads the card.
	out.DependenciesMet, out.InPlaybooks = nil, nil
	out.SubtaskCostUSD, out.SubtaskCostHasEstimates = 0, false

	body, bodyRes, bodyAudit := mergeBody(base, ours, theirs, path, oursLater, c)
	out.Body = body

	res = append(res, bodyRes...)
	audits = append(audits, bodyAudit...)

	out.ActivityLog = unionActivity(ours.ActivityLog, theirs.ActivityLog, audits)

	out.Updated = theirs.Updated
	if oursLater {
		out.Updated = ours.Updated
	}

	return &out, res
}

func mergeBody(base, ours, theirs *board.Card, path string, oursLater bool, c Context) (string, []Resolution, []board.ActivityEntry) {
	switch {
	case ours.Body == theirs.Body:
		return ours.Body, nil, nil
	case ours.Body == base.Body:
		return theirs.Body, nil, nil
	case theirs.Body == base.Body:
		return ours.Body, nil, nil
	}

	if merged, clean := c.MergeBody(base.Body, ours.Body, theirs.Body); clean {
		return merged, nil, nil
	}

	body, loserCommit := theirs.Body, c.OursCommit
	if oursLater {
		body, loserCommit = ours.Body, c.TheirsCommit
	}

	res := []Resolution{{
		Path: path, CardID: ours.ID, Rule: RuleBodyLaterUpdated,
		Detail: "overridden body is in commit " + loserCommit,
	}}
	entry := auditEntry(c, RuleBodyLaterUpdated,
		"body from "+loserSide(oursLater)+" overridden; text kept in commit "+loserCommit)

	return body, res, []board.ActivityEntry{entry}
}

// sideLocal and sideRemote name the side an audit entry reports as overridden.
const (
	sideLocal  = "local"
	sideRemote = "remote"
)

func loserSide(oursLater bool) string {
	if oursLater {
		return sideRemote
	}

	return sideLocal
}

func firstNonNil(a, b *board.Source) *board.Source {
	if a != nil {
		return a
	}

	return b
}

// pickLater is the three-way pick with the later-updated side as the tiebreak.
func pickLater[T comparable](b, o, t T, oursLater bool, audit func(rule, field, losing string), field string) T {
	v, conflict := pick(b, o, t)
	if !conflict {
		return v
	}

	audit(RuleLaterUpdated, field, loserSide(oursLater))

	if oursLater {
		return o
	}

	return t
}

func pickHeartbeat(b, o, t *time.Time) *time.Time {
	eq := func(a, b *time.Time) bool {
		if a == nil || b == nil {
			return a == b
		}

		return a.Equal(*b)
	}

	v, conflict := pickEq(b, o, t, eq)
	if !conflict {
		return v
	}

	if o != nil && (t == nil || o.After(*t)) { // the newest heartbeat wins
		return o
	}

	return t
}

// mergeSkills unions the two lists when both sides declare one. The field is a
// tri-state pointer - nil inherits the project default - so a side that has no
// list falls back to the plain three-way pick on the pointer.
func mergeSkills(base, ours, theirs *[]string) *[]string {
	if ours == nil || theirs == nil {
		v, _ := pickEq(base, ours, theirs, func(a, b *[]string) bool { return reflect.DeepEqual(a, b) })

		return v
	}

	var baseList []string
	if base != nil {
		baseList = *base
	}

	merged := mergeSet(baseList, *ours, *theirs, nil)
	if merged == nil {
		merged = []string{}
	}

	return &merged
}

func mapRef(ref, baseRef, project string, renames map[string]string) string {
	if ref == "" || ref == baseRef {
		return ref
	}

	if n, ok := renames[project+"/"+ref]; ok {
		return n
	}

	return ref
}

func projectRenames(project string, renames map[string]string) map[string]string {
	out := map[string]string{}

	for k, v := range renames {
		if strings.HasPrefix(k, project+"/") {
			out[strings.TrimPrefix(k, project+"/")] = v
		}
	}

	return out
}

func mergeCustom(b, o, t map[string]any, oursLater bool, audit func(rule, field, losing string)) map[string]any {
	keys := map[string]bool{}

	for _, m := range []map[string]any{b, o, t} {
		for k := range m {
			keys[k] = true
		}
	}

	if len(keys) == 0 {
		return nil
	}

	out := map[string]any{}

	for _, k := range slices.Sorted(maps.Keys(keys)) {
		bv, bok := b[k]
		ov, ook := o[k]
		tv, tok := t[k]

		switch {
		case ook == tok && reflect.DeepEqual(ov, tv):
			if ook {
				out[k] = ov
			}
		case ook == bok && reflect.DeepEqual(ov, bv):
			if tok {
				out[k] = tv
			}
		case tok == bok && reflect.DeepEqual(tv, bv):
			if ook {
				out[k] = ov
			}
		default:
			audit(RuleLaterUpdated, "custom."+k, loserSide(oursLater))

			switch {
			case oursLater && ook:
				out[k] = ov
			case !oursLater && tok:
				out[k] = tv
			}
		}
	}

	return out
}

func mergeTokenUsage(b, o, t *board.TokenUsage) *board.TokenUsage {
	if o == nil && t == nil {
		return nil
	}

	if b == nil {
		b = &board.TokenUsage{}
	}

	if o == nil {
		o = b
	}

	if t == nil {
		t = b
	}

	out := board.TokenUsage{
		PromptTokens:        add3(b.PromptTokens, o.PromptTokens, t.PromptTokens),
		CompletionTokens:    add3(b.CompletionTokens, o.CompletionTokens, t.CompletionTokens),
		CacheReadTokens:     add3(b.CacheReadTokens, o.CacheReadTokens, t.CacheReadTokens),
		CacheCreationTokens: add3(b.CacheCreationTokens, o.CacheCreationTokens, t.CacheCreationTokens),
		EstimatedCostUSD:    add3(b.EstimatedCostUSD, o.EstimatedCostUSD, t.EstimatedCostUSD),
	}
	out.Model, _ = pick(b.Model, o.Model, t.Model)

	return &out
}

// mergeBuckets adds both sides' deltas per (agent, model) bucket. A bucket
// absent from the ancestor and identical on both sides is one seed written
// twice, so it counts once.
func mergeBuckets(b, o, t []board.UsageBucket) []board.UsageBucket {
	type key struct{ agent, model string }

	index := func(xs []board.UsageBucket) map[key]board.UsageBucket {
		m := make(map[key]board.UsageBucket, len(xs))
		for _, x := range xs {
			m[key{x.Agent, x.Model}] = x
		}

		return m
	}

	bm, om, tm := index(b), index(o), index(t)

	var order []key

	seen := map[key]bool{}

	for _, xs := range [][]board.UsageBucket{t, o} {
		for _, x := range xs {
			k := key{x.Agent, x.Model}
			if !seen[k] {
				seen[k] = true
				order = append(order, k)
			}
		}
	}

	if len(order) == 0 {
		return nil
	}

	out := make([]board.UsageBucket, 0, len(order))

	for _, k := range order {
		bv, inBase := bm[k]
		ov, tv := om[k], tm[k]

		if !inBase && reflect.DeepEqual(ov, tv) {
			out = append(out, ov)

			continue
		}

		merged := board.UsageBucket{
			Agent: k.agent, Model: k.model,
			PromptTokens:        add3(bv.PromptTokens, ov.PromptTokens, tv.PromptTokens),
			CompletionTokens:    add3(bv.CompletionTokens, ov.CompletionTokens, tv.CompletionTokens),
			CacheReadTokens:     add3(bv.CacheReadTokens, ov.CacheReadTokens, tv.CacheReadTokens),
			CacheCreationTokens: add3(bv.CacheCreationTokens, ov.CacheCreationTokens, tv.CacheCreationTokens),
			CostUSD:             add3(bv.CostUSD, ov.CostUSD, tv.CostUSD),
		}
		copyBucketExtras(&merged, ov, tv)

		out = append(out, merged)
	}

	return out
}

// copyBucketExtras carries the provenance fields across. Both are sticky: a
// cost measured by the provider and counts read by the collector stay that way
// however the other side labelled its own delta.
func copyBucketExtras(dst *board.UsageBucket, ours, theirs board.UsageBucket) {
	dst.CostSource = stickiest(ours.CostSource, theirs.CostSource, "actual")
	dst.CountsSource = stickiest(ours.CountsSource, theirs.CountsSource, "collector")
}

func stickiest(ours, theirs, sticky string) string {
	if ours == sticky || theirs == sticky {
		return sticky
	}

	if ours != "" {
		return ours
	}

	return theirs
}

// unionActivity keeps every entry either side has, de-duplicated by identity,
// oldest first and trimmed to the cap. The log is append-only, so an entry
// missing from one side was added or trimmed, never deleted.
func unionActivity(ours, theirs, extra []board.ActivityEntry) []board.ActivityEntry {
	type key struct {
		ts                     int64
		agent, action, message string
	}

	seen := map[key]bool{}

	var out []board.ActivityEntry

	for _, list := range [][]board.ActivityEntry{theirs, ours, extra} {
		for _, e := range list {
			k := key{e.Timestamp.UnixNano(), e.Agent, e.Action, e.Message}
			if !seen[k] {
				out = append(out, e)
				seen[k] = true
			}
		}
	}

	slices.SortStableFunc(out, func(a, b board.ActivityEntry) int { return a.Timestamp.Compare(b.Timestamp) })

	return board.TrimActivityLog(out)
}
