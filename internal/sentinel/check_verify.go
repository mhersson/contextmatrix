package sentinel

import (
	"fmt"
	"slices"
	"strings"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/board"
)

// checkVerify applies the verify rule: env names only removed, a changed
// command must be a declared candidate, and the trial run must have finished.
func checkVerify(c fixContext) []string {
	if c.Fix.Verify == nil {
		return nil
	}

	before := resolved(c.Card.Verify, c.Project)
	after := resolved(&board.VerifyConfig{
		Command:        strings.TrimSpace(c.Fix.Verify.Command),
		TimeoutSeconds: c.Fix.Verify.TimeoutSeconds,
		Env:            c.Fix.Verify.Env,
	}, c.Project)

	var violations []string

	for _, name := range after.Env {
		if !slices.Contains(before.Env, name) {
			violations = append(violations, "adds env "+name)
		}
	}

	switch {
	case before.Command != "" && after.Command == "":
		violations = append(violations, "removes the verify command")
	case !sameFields(before.Command, after.Command) && !declared(c.Check, after.Command):
		violations = append(violations, "command not declared by the repo")
	}

	switch {
	case c.Check == nil:
		violations = append(violations, "verify not trial-run")
	case c.Check.Trial != protocol.TrialFinished:
		violations = append(violations, fmt.Sprintf("trial run: %s", c.Check.Trial))
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

func declared(check *protocol.CheckResult, command string) bool {
	if check == nil {
		return false
	}

	return slices.ContainsFunc(check.Candidates, func(cand string) bool {
		return sameFields(cand, command)
	})
}

func sameFields(a, b string) bool {
	return slices.Equal(strings.Fields(a), strings.Fields(b))
}
