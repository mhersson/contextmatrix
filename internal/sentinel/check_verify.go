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

	before := board.ResolveVerify(c.Card.Verify, c.Project)
	after := board.ResolveVerify(&board.VerifyConfig{
		Command:        c.Fix.Verify.Command,
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
