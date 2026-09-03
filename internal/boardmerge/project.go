package boardmerge

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/mhersson/contextmatrix/internal/board"
)

// resolveProject merges one project's .board.yaml three-way. next_id takes
// the max of both sides so no minted card ID is ever reused; states, types,
// priorities and transitions merge as unions so a card that landed in a
// value one side dropped stays valid. name and prefix are immutable:
// any difference is audited and remote is kept. Everything else is a
// three-way scalar or deep pick with remote winning a real conflict, since a
// project config carries no updated timestamp to break ties by recency.
func resolveProject(in Input, project string, _ Context) (Output, error) {
	if in.Ours == nil || in.Theirs == nil {
		return Output{Resolutions: []Resolution{{
			Path: in.Path, Rule: RuleDeleteWins, Detail: "project config deleted on one side",
		}}, Deleted: true}, nil
	}

	ours, oursErr := board.ParseProjectConfig(in.Ours)
	theirs, theirsErr := board.ParseProjectConfig(in.Theirs)

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

	base := &board.ProjectConfig{}

	if in.Base != nil {
		if b, err := board.ParseProjectConfig(in.Base); err == nil {
			base = b
		}
	}

	var res []Resolution

	out := *theirs // start from remote; name and prefix stay remote's value

	if ours.Name != theirs.Name || ours.Prefix != theirs.Prefix {
		res = append(res, Resolution{
			Path: in.Path, Rule: RuleProjectImmutable,
			Detail: fmt.Sprintf("name/prefix differ (local %s/%s, remote %s/%s); remote kept",
				ours.Name, ours.Prefix, theirs.Name, theirs.Prefix),
		})
	}

	out.NextID = max(ours.NextID, theirs.NextID)
	out.States = unionOrdered(base.States, ours.States, theirs.States)
	out.Types = unionOrdered(base.Types, ours.Types, theirs.Types)
	out.Priorities = unionOrdered(base.Priorities, ours.Priorities, theirs.Priorities)

	out.Transitions = map[string][]string{}
	for _, k := range unionOrdered(nil, mapKeys(ours.Transitions), mapKeys(theirs.Transitions)) {
		out.Transitions[k] = unionOrdered(base.Transitions[k], ours.Transitions[k], theirs.Transitions[k])
	}

	theirsWins := func(field string) {
		res = append(res, Resolution{Path: in.Path, Rule: RuleLaterUpdated, Detail: field + ": both sides changed; remote kept"})
	}

	out.DisplayName = pickStr(base.DisplayName, ours.DisplayName, theirs.DisplayName, theirsWins, "display_name")
	out.Repo = pickStr(base.Repo, ours.Repo, theirs.Repo, theirsWins, "repo")
	out.GitHubCredential = pickStr(base.GitHubCredential, ours.GitHubCredential, theirs.GitHubCredential, theirsWins, "github_credential")
	out.Repos = pickDeep(base.Repos, ours.Repos, theirs.Repos, theirsWins, "repos")
	out.RemoteExecution = pickDeep(base.RemoteExecution, ours.RemoteExecution, theirs.RemoteExecution, theirsWins, "remote_execution")
	out.GitHub = pickDeep(base.GitHub, ours.GitHub, theirs.GitHub, theirsWins, "github")
	out.DefaultSkills = pickDeep(base.DefaultSkills, ours.DefaultSkills, theirs.DefaultSkills, theirsWins, "default_skills")
	out.Verify = pickDeep(base.Verify, ours.Verify, theirs.Verify, theirsWins, "verify")
	out.Favorites = pickDeep(base.Favorites, ours.Favorites, theirs.Favorites, theirsWins, "favorites")

	data, err := board.SerializeProjectConfig(&out)
	if err != nil {
		return Output{}, fmt.Errorf("serialize merged project config %s: %w", project, err)
	}

	return Output{Content: data, Resolutions: res}, nil
}

// unionOrdered keeps every value present in either list, remote first then
// local additions, deduplicated. It never drops a value one side removed: a
// card that already landed in that value must stay valid. base is accepted
// for symmetry with the other three-way merge helpers but unused: unlike
// mergeSet's set-difference semantics, a union has no use for the ancestor.
func unionOrdered(_, ours, theirs []string) []string {
	var out []string

	seen := map[string]bool{}

	for _, list := range [][]string{theirs, ours} {
		for _, v := range list {
			if !seen[v] {
				seen[v] = true

				out = append(out, v)
			}
		}
	}

	return out
}

func mapKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

func pickStr(base, ours, theirs string, onConflict func(string), field string) string {
	v, conflict := pick(base, ours, theirs)
	if conflict {
		onConflict(field)

		return theirs
	}

	return v
}

func pickDeep[T any](base, ours, theirs T, onConflict func(string), field string) T {
	v, conflict := pickEq(base, ours, theirs, func(a, b T) bool { return reflect.DeepEqual(a, b) })
	if conflict {
		onConflict(field)

		return theirs
	}

	return v
}
