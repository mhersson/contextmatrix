package sentinel

import (
	"fmt"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/service"
)

// checkVerify applies the verify rule: env names only removed, a changed
// command must be a declared candidate, and the trial run must have finished.
// The card service's own verify rules apply too, so a fix it would refuse is
// rejected before anything is written.
func checkVerify(c fixContext) []string {
	if c.Fix.Verify == nil {
		return nil
	}

	before := resolved(c.Card.Verify, c.Project)
	after := resolvedFix(c.Fix.Verify, c.Project)

	var violations []string

	for _, name := range after.Env {
		if !slices.Contains(before.Env, name) {
			violations = append(violations, "adds env "+name)
		}
	}

	switch {
	case before.Command != "" && after.Command == "":
		violations = append(violations, "removes the verify command")
	case !sameCommand(before.Command, after.Command) && !declared(c.Check, after.Command):
		violations = append(violations, "command not declared by the repo")
	}

	switch {
	case c.Check == nil:
		violations = append(violations, "verify not trial-run")
	case c.Check.Trial != protocol.TrialFinished:
		violations = append(violations, fmt.Sprintf("trial run: %s", c.Check.Trial))
	}

	if err := service.ValidateVerifyConfig(boardVerify(c.Fix.Verify)); err != nil {
		violations = append(violations, "verify "+err.Error())
	}

	return violations
}

// resolved is the verify config as the card will resolve it. A card verify
// with no intent (such as an empty env list) is stored as nil, so the card
// inherits the project's; the service's normalizeVerify does the same on write.
func resolved(card, project *board.VerifyConfig) board.VerifyConfig {
	if card.IsZero() {
		card = nil
	}

	if r := board.ResolveVerify(card, project); r != nil {
		return *r
	}

	return board.VerifyConfig{}
}

// resolvedFix is a fix's verify as the card will resolve it once written.
func resolvedFix(fix *protocol.VerifyConfig, project *board.VerifyConfig) board.VerifyConfig {
	v := boardVerify(fix)
	v.Command = strings.TrimSpace(v.Command)

	return resolved(v, project)
}

// boardVerify is a fix's verify as the card service is asked to write it.
func boardVerify(fix *protocol.VerifyConfig) *board.VerifyConfig {
	return &board.VerifyConfig{Command: fix.Command, TimeoutSeconds: fix.TimeoutSeconds, Env: fix.Env}
}

func declared(check *protocol.CheckResult, command string) bool {
	if check == nil {
		return false
	}

	return slices.ContainsFunc(check.Candidates, func(cand string) bool {
		return sameCommand(cand, command)
	})
}

// sameCommand matches the agent's rule: equal non-empty lines, each with equal
// whitespace-separated fields. A newline is significant: bash runs each line
// as its own command.
func sameCommand(a, b string) bool {
	return slices.EqualFunc(commandLines(a), commandLines(b), slices.Equal[[]string])
}

func commandLines(cmd string) [][]string {
	var out [][]string

	for line := range strings.Lines(cmd) {
		if f := strings.Fields(line); len(f) > 0 {
			out = append(out, f)
		}
	}

	return out
}
