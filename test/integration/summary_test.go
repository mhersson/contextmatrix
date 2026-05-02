//go:build integration

package integration_test

import (
	"fmt"
	"os"
	"sync"
)

// summaryRow is one line in the final summary table.
type summaryRow struct {
	mode      string // "Stub" | "Real-Claude"
	scenario  string
	pass      bool
	skipped   bool
	durationS string // e.g. "3.2s"
	costNote  string // e.g. "~14k tokens ~$0.04" (real only)
	frictionN int    // number of findings (real only, 0 = clean)
}

var (
	summaryMu   sync.Mutex
	summaryRows []summaryRow
)

func recordSummary(row summaryRow) {
	summaryMu.Lock()
	defer summaryMu.Unlock()

	summaryRows = append(summaryRows, row)
}

// printSummary writes the harness summary to stderr. Called from
// TestMain after m.Run() but before exit.
func printSummary() {
	summaryMu.Lock()
	defer summaryMu.Unlock()

	if len(summaryRows) == 0 {
		return
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Integration test summary")
	fmt.Fprintln(os.Stderr, "========================")

	stubRows := filterRows(summaryRows, "Stub")
	realRows := filterRows(summaryRows, "Real-Claude")

	if len(stubRows) > 0 {
		fmt.Fprintln(os.Stderr, "Stub mode:")

		for _, r := range stubRows {
			fmt.Fprintf(os.Stderr, "  %-22s %s  %s\n",
				r.scenario, statusBadge(r), r.durationS)
		}
	}

	if len(realRows) > 0 {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Real-Claude mode:")

		for _, r := range realRows {
			fmt.Fprintf(os.Stderr, "  %-22s %s  %s   %s   friction findings: %d\n",
				r.scenario, statusBadge(r), r.durationS, r.costNote, r.frictionN)
		}
	}

	totals := tally(summaryRows)
	fmt.Fprintf(os.Stderr, "\nTotal: %d PASS, %d SKIP, %d FAIL\n",
		totals.pass, totals.skip, totals.fail)
}

func filterRows(rows []summaryRow, mode string) []summaryRow {
	var out []summaryRow

	for _, r := range rows {
		if r.mode == mode {
			out = append(out, r)
		}
	}

	return out
}

func statusBadge(r summaryRow) string {
	switch {
	case r.skipped:
		return "SKIP"
	case r.pass:
		return "PASS"
	default:
		return "FAIL"
	}
}

type talliedTotals struct {
	pass, skip, fail int
}

func tally(rows []summaryRow) talliedTotals {
	var t talliedTotals

	for _, r := range rows {
		switch {
		case r.skipped:
			t.skip++
		case r.pass:
			t.pass++
		default:
			t.fail++
		}
	}

	return t
}
