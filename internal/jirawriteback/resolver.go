package jirawriteback

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mhersson/contextmatrix/internal/config"
	"github.com/mhersson/contextmatrix/internal/jira"
)

type outcome int

const (
	outcomeDone outcome = iota
	outcomeSkipped
)

// errNoTransition is permanent: the workflow offers no unique path, so
// jira.writeback.status_targets must name the status.
var errNoTransition = errors.New("no unique transition")

const maxHops = 3

var categoryRank = map[string]int{"new": 0, "indeterminate": 1, "done": 2}

type cacheKey struct {
	project, issueType, from, kind string
}

type resolver struct {
	api     JiraAPI
	targets map[string]config.JiraStatusTarget
	cache   map[cacheKey]jira.Transition
}

func newResolver(api JiraAPI, targets map[string]config.JiraStatusTarget) *resolver {
	return &resolver{api: api, targets: targets, cache: map[cacheKey]jira.Transition{}}
}

func targetCategory(kind string) string {
	if kind == KindDone {
		return "done"
	}

	return "indeterminate"
}

// needsWrite is the status guard: started only leaves a To Do-category
// status and done only acts when not done, so a human-closed issue is never
// reopened and a duplicate write is a no-op.
func needsWrite(kind, category string) bool {
	if kind == KindDone {
		return category != "done"
	}

	return category == "new"
}

// apply moves the issue for kind, walking at most maxHops transitions.
func (r *resolver) apply(ctx context.Context, key, kind string) (outcome, error) {
	issue, err := r.api.Issue(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("read issue: %w", err)
	}

	if !needsWrite(kind, issue.Status.CategoryKey) {
		return outcomeSkipped, nil
	}

	target := r.targets[issue.ProjectKey]

	wantName := target.Started
	if kind == KindDone {
		wantName = target.Done
	}

	resolution := target.Resolution
	if resolution == "" {
		resolution = "Done"
	}

	wantCat := targetCategory(kind)
	cur := issue.Status
	visited := map[string]bool{strings.ToLower(cur.Name): true}
	retriedStale := false

	for hop := 0; hop < maxHops; {
		ck := cacheKey{issue.ProjectKey, issue.IssueType.Name, cur.Name, kind}

		next, cached := r.cache[ck]
		if !cached {
			trs, err := r.api.Transitions(ctx, key)
			if err != nil {
				return 0, fmt.Errorf("list transitions: %w", err)
			}

			next, err = pick(trs, cur, wantCat, wantName, visited)
			if err != nil {
				return 0, err
			}

			r.cache[ck] = next
		}

		if err := r.api.Transition(ctx, key, next.ID, fieldsFor(next, resolution)); err != nil {
			delete(r.cache, ck)

			if errors.Is(err, jira.ErrUnauthorized) {
				return 0, fmt.Errorf("transition: %w", err)
			}

			// The issue may have moved meanwhile (another instance, a human,
			// a Jira automation): already at the target counts as success.
			now, rerr := r.api.Issue(ctx, key)
			if rerr != nil {
				if errors.Is(rerr, jira.ErrUnauthorized) {
					return 0, fmt.Errorf("re-read after transition: %w", rerr)
				}

				return 0, fmt.Errorf("transition %q: %w", next.Name, err)
			}

			if !needsWrite(kind, now.Status.CategoryKey) {
				return outcomeDone, nil
			}

			// A cached transition goes stale when the workflow changes:
			// resolve afresh once from the status Jira reports now.
			if !cached || retriedStale {
				return 0, fmt.Errorf("transition %q: %w", next.Name, err)
			}

			retriedStale = true
			cur = now.Status

			continue
		}

		if reached(next.To, wantCat, wantName) {
			return outcomeDone, nil
		}

		visited[strings.ToLower(next.To.Name)] = true
		cur = next.To
		hop++
	}

	return 0, fmt.Errorf("%w: %s not reached within %d transitions", errNoTransition, wantCat, maxHops)
}

func reached(to jira.Status, wantCat, wantName string) bool {
	if wantName != "" {
		return strings.EqualFold(to.Name, wantName)
	}

	return to.CategoryKey == wantCat
}

// pick chooses the next transition: the configured status by name, else the
// single transition into the target category, else the single forward step
// toward it. Anything else is ambiguous.
func pick(trs []jira.Transition, cur jira.Status, wantCat, wantName string, visited map[string]bool) (jira.Transition, error) {
	if wantName != "" {
		for _, t := range trs {
			if strings.EqualFold(t.To.Name, wantName) {
				return t, nil
			}
		}
	} else {
		final := filter(trs, func(t jira.Transition) bool { return t.To.CategoryKey == wantCat })

		switch len(final) {
		case 1:
			return final[0], nil
		case 0:
		default:
			return jira.Transition{}, fmt.Errorf("%w: %d transitions lead to %s (%s); set jira.writeback.status_targets",
				errNoTransition, len(final), wantCat, names(final))
		}
	}

	curRank := categoryRank[cur.CategoryKey]
	wantRank := categoryRank[wantCat]

	steps := filter(trs, func(t jira.Transition) bool {
		r, ok := categoryRank[t.To.CategoryKey]

		return ok && r >= curRank && r < wantRank && !visited[strings.ToLower(t.To.Name)]
	})

	switch len(steps) {
	case 1:
		return steps[0], nil
	case 0:
		return jira.Transition{}, fmt.Errorf("%w: no transition from %q toward %s", errNoTransition, cur.Name, wantCat)
	default:
		return jira.Transition{}, fmt.Errorf("%w: %d possible steps from %q (%s); set jira.writeback.status_targets",
			errNoTransition, len(steps), cur.Name, names(steps))
	}
}

func fieldsFor(t jira.Transition, resolution string) map[string]any {
	if f, ok := t.Fields["resolution"]; ok && f.Required {
		return map[string]any{"resolution": map[string]string{"name": resolution}}
	}

	return nil
}

func filter(trs []jira.Transition, keep func(jira.Transition) bool) []jira.Transition {
	var out []jira.Transition

	for _, t := range trs {
		if keep(t) {
			out = append(out, t)
		}
	}

	return out
}

func names(trs []jira.Transition) string {
	ns := make([]string, 0, len(trs))
	for _, t := range trs {
		ns = append(ns, t.To.Name)
	}

	return strings.Join(ns, ", ")
}
