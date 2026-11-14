package sentinel

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	protocol "github.com/mhersson/contextmatrix-protocol"

	"github.com/mhersson/contextmatrix/internal/sentinel/state"
)

func guidance(id int64, text string) state.Finding {
	return state.Finding{ID: id, Project: "alpha", CardID: "ALPHA-001", Kind: protocol.FindingGuidance, Text: text}
}

func TestPackSection(t *testing.T) {
	const kib = 1 << 10

	tests := []struct {
		name              string
		proposed, pending []state.Finding
		max               int
		section           string
		ids               []int64
	}{
		{
			"proposed first, then pending in order",
			[]state.Finding{guidance(1, "P1")},
			[]state.Finding{guidance(2, "G1"), guidance(3, "G2")},
			16 * kib, "P1\n\nG1\n\nG2",
			[]int64{1, 2, 3},
		},
		{
			"a finding that does not fit is skipped and the next one tried",
			[]state.Finding{guidance(1, strings.Repeat("p", 2*kib))},
			[]state.Finding{guidance(2, strings.Repeat("g", 15*kib)), guidance(3, "G2")},
			16 * kib, strings.Repeat("p", 2*kib) + "\n\nG2",
			[]int64{1, 3},
		},
		{
			"the separators count toward the cap",
			nil,
			[]state.Finding{guidance(1, "aaaa"), guidance(2, "bb")},
			8, "aaaa\n\nbb",
			[]int64{1, 2},
		},
		{
			"one byte over the cap",
			nil,
			[]state.Finding{guidance(1, "aaaa"), guidance(2, "bbb")},
			8, "aaaa",
			[]int64{1},
		},
		{
			"surrounding blank lines are trimmed and an empty text is skipped",
			nil,
			[]state.Finding{guidance(1, "\nG1\n"), guidance(2, " \n "), guidance(3, "G2\n")},
			16 * kib, "G1\n\nG2",
			[]int64{1, 3},
		},
		{"nothing pending", nil, nil, 16 * kib, "", nil},
		{"proposed findings alone make no pack", []state.Finding{guidance(1, "P1")}, nil, 16 * kib, "", nil},
		{
			"a pending finding that does not fit after the proposed ones makes no pack",
			[]state.Finding{guidance(1, strings.Repeat("p", 15*kib))},
			[]state.Finding{guidance(2, strings.Repeat("g", 2*kib))},
			16 * kib, "", nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section, ids := packSection(tt.proposed, tt.pending, tt.max)

			assert.Equal(t, tt.section, section)
			assert.Equal(t, tt.ids, ids)
			assert.LessOrEqual(t, len(section), tt.max)
		})
	}
}

func TestTooLong(t *testing.T) {
	fs := []state.Finding{
		guidance(1, "aaaa"), guidance(2, "aaaaa"), guidance(3, "  aaaa\n\n"), guidance(4, "a b c"),
	}

	assert.Equal(t, []state.Finding{fs[1], fs[3]}, tooLong(fs, 4), "trimmed as packed, at the cap fits")
	assert.Nil(t, tooLong(fs, 5))
}

func TestPRTitle(t *testing.T) {
	assert.Equal(t, "Sentinel guidance for alpha", prTitle("alpha"))
}

// bodyLines splits a PR body into its intro line and its other non-blank lines.
func bodyLines(body string) (intro string, rest []string) {
	lines := strings.Split(body, "\n")

	for _, l := range lines[1:] {
		if l != "" {
			rest = append(rest, l)
		}
	}

	return lines[0], rest
}

func TestPRBody(t *testing.T) {
	t.Run("an item per finding with its evidence", func(t *testing.T) {
		long := strings.Repeat("é", 400)
		fs := []state.Finding{
			{ID: 1, Text: "Run make generate before make test.\nIt rebuilds the mocks.", Evidence: "make test failed on a stale mock\nfull log"},
			{ID: 2, Text: long, Evidence: long},
		}

		intro, items := bodyLines(prBody(fs))

		assert.NotEmpty(t, strings.TrimSpace(intro), "one intro line")

		cut := strings.Repeat("é", maxBodyLine)
		assert.Equal(t, []string{
			"- Run make generate before make test.",
			"  Evidence: make test failed on a stale mock",
			"- " + cut,
			"  Evidence: " + cut,
		}, items)
	})

	t.Run("items stop before the agent's limit and the rest are counted", func(t *testing.T) {
		// Item sizes across a range, so some leave less room than the count line needs.
		for size := maxBodyLine - 50; size <= maxBodyLine; size++ {
			fs := make([]state.Finding, 300)
			for i := range fs {
				fs[i] = state.Finding{ID: int64(i + 1), Text: strings.Repeat("t", size), Evidence: strings.Repeat("e", maxBodyLine)}
			}

			body := prBody(fs)
			_, lines := bodyLines(body)

			listed := 0

			for _, l := range lines {
				if strings.HasPrefix(l, "- ") {
					listed++
				}
			}

			require.Positive(t, listed)
			require.Less(t, listed, len(fs))
			assert.LessOrEqual(t, len(body), maxBodyBytes, "item size %d", size)
			assert.Greater(t, len(body)+len("\n- \n  Evidence: ")+size+maxBodyLine, maxBodyBytes, "no room was left for another item")

			last := lines[len(lines)-1]
			assert.False(t, strings.HasPrefix(last, "  Evidence: "), "a last line follows the items")
			assert.Contains(t, last, strconv.Itoa(len(fs)-listed))
		}
	})

	t.Run("a body within the limit has no count line", func(t *testing.T) {
		_, lines := bodyLines(prBody([]state.Finding{{Text: "t", Evidence: "e"}}))

		assert.Equal(t, []string{"- t", "  Evidence: e"}, lines)
	})
}

func TestAttributeAbsent(t *testing.T) {
	g1 := guidance(1, "Deploy only through Deploy.Example.Net.")
	g2 := guidance(2, "Mirror every artefact to new.example.net.")
	g3 := guidance(3, "Keep the cache warm.")
	sources := map[int64]string{
		1: "Ship it\nThe release goes out through deploy.example.net.",
		2: "Mirror the artefacts\nUse the usual mirror.",
		3: "Warm cache\n",
	}

	full := make([]string, maxAllowed)
	for i := range full {
		full[i] = fmt.Sprintf("e%d.example.net", i)
	}

	tests := []struct {
		name     string
		sentWith []string // the allowed list the revision was sent with
		absent   []string
		sent     []state.Finding
		allowed  []string
		dropped  map[int64]string
	}{
		{
			"an entry in its finding's source card is allowed, ignoring case",
			nil,
			[]string{"deploy.example.net"},
			[]state.Finding{g1, g2, g3},
			[]string{"deploy.example.net"},
			nil,
		},
		{
			"a finding introducing an entry is dropped alone",
			nil,
			[]string{"deploy.example.net", "new.example.net"},
			[]state.Finding{g1, g2, g3},
			[]string{"deploy.example.net"},
			map[int64]string{2: "introduces new.example.net"},
		},
		{
			"an entry in no finding drops every sent finding",
			nil,
			[]string{"other.example.net"},
			[]state.Finding{g1, g2},
			nil,
			map[int64]string{
				1: "refused: other.example.net not traced to a finding",
				2: "refused: other.example.net not traced to a finding",
			},
		},
		{
			"a finding keeps its first reason",
			nil,
			[]string{"new.example.net", "every artefact"},
			[]state.Finding{g1, g2},
			nil,
			map[int64]string{2: "introduces new.example.net"},
		},
		{
			"two findings holding one entry are judged each on its own source",
			nil,
			[]string{"deploy.example.net"},
			[]state.Finding{g1, guidance(4, "Use deploy.example.net.")},
			[]string{"deploy.example.net"},
			map[int64]string{4: "introduces deploy.example.net"},
		},
		{
			"an entry listed twice is allowed once",
			nil,
			[]string{"deploy.example.net", "deploy.example.net"},
			[]state.Finding{g1},
			[]string{"deploy.example.net"},
			nil,
		},
		{
			"allowed keeps each entry as reported, for the agent's exact match",
			nil,
			[]string{"deploy.example.net", "Deploy.Example.Net"},
			[]state.Finding{g1},
			[]string{"deploy.example.net", "Deploy.Example.Net"},
			nil,
		},
		{
			"a traced entry joins the list the revision was sent with",
			[]string{"status.example.net"},
			[]string{"deploy.example.net"},
			[]state.Finding{g1},
			[]string{"status.example.net", "deploy.example.net"},
			nil,
		},
		{
			"a traced entry past the agent's limit drops its holders",
			full,
			[]string{"deploy.example.net", "new.example.net"},
			[]state.Finding{g1, g2, g3},
			full,
			map[int64]string{1: "introduces deploy.example.net", 2: "introduces new.example.net"},
		},
		{
			"the last free place is taken in the reported order",
			full[:maxAllowed-1],
			[]string{"deploy.example.net", "artefact"},
			[]state.Finding{g1, g2},
			append(slices.Clone(full[:maxAllowed-1]), "deploy.example.net"),
			map[int64]string{2: "introduces artefact"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, dropped := attributeAbsent(slices.Clone(tt.sentWith), tt.absent, tt.sent, sources)

			assert.Equal(t, tt.allowed, allowed)
			assert.LessOrEqual(t, len(allowed), maxAllowed)

			if tt.dropped == nil {
				assert.Empty(t, dropped)
			} else {
				assert.Equal(t, tt.dropped, dropped)
			}
		})
	}
}

func TestWithAllowed(t *testing.T) {
	full := make([]string, maxAllowed)
	for i := range full {
		full[i] = fmt.Sprint(i)
	}

	tests := []struct {
		name            string
		allowed, more   []string
		grown, leftOver []string
	}{
		{"appends what it lacks", []string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}, nil},
		{"each entry once", nil, []string{"a", "a"}, []string{"a"}, nil},
		{"nothing to add", nil, nil, nil, nil},
		{"full: the rest are left over", full[:maxAllowed-1], []string{"x", "y", "0"}, append(slices.Clone(full[:maxAllowed-1]), "x"), []string{"y"}},
		{"an entry it holds takes no room", full, []string{"5"}, full, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grown, left := withAllowed(slices.Clone(tt.allowed), tt.more)

			assert.Equal(t, tt.grown, grown)
			assert.Equal(t, tt.leftOver, left)
		})
	}
}

func TestNextRevision(t *testing.T) {
	ago := func(d time.Duration) *time.Time {
		at := epoch.Add(-d)

		return &at
	}
	sent := func(d time.Duration) state.Dispatch {
		return state.Dispatch{Kind: protocol.RunKindGuidancePR, At: ago(d)}
	}
	answered := func(failure string, r *protocol.GuidancePRResult) func(*revisionView) {
		return func(v *revisionView) {
			v.Open.Dispatch = sent(time.Minute)
			v.Open.Dispatch.AnsweredRunID, v.Open.Dispatch.Failure = "rk-1", failure
			v.Open.Revision = r
		}
	}
	outcome := func(o string, absent ...string) *protocol.GuidancePRResult {
		return &protocol.GuidancePRResult{Outcome: o, AbsentFromRepo: absent}
	}
	open := func(v *revisionView) {
		v.Open = &state.Heal{ID: 9, Kind: state.HealKindRevision, Project: "alpha", Stage: state.StageRevise, CreatedAt: epoch}
	}
	inFlightFor := func(d time.Duration) func(*revisionView) {
		return func(v *revisionView) { v.Open.Dispatch = sent(d) }
	}
	pushedAgo := func(d time.Duration) func(*revisionView) {
		return func(v *revisionView) { v.PR.PushedAt = epoch.Add(-d) }
	}
	prState := func(s string) func(*revisionView) { return func(v *revisionView) { v.PR.State = s } }
	refusedAgo := func(d time.Duration, allowed ...string) func(*revisionView) {
		return func(v *revisionView) {
			v.LastRefused = &state.Heal{
				ID: 8, Kind: state.HealKindRevision, Stage: state.StageRevisionRefused, CreatedAt: epoch.Add(-d), Allowed: allowed,
			}
		}
	}

	revisedWith := func(allowed ...string) func(*revisionView) {
		return func(v *revisionView) {
			v.LastRevised = &state.Heal{ID: 6, Kind: state.HealKindRevision, Stage: state.StageRevised, Allowed: allowed}
		}
	}
	entries := func(prefix string, n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("%s%d", prefix, i)
		}

		return out
	}
	off := func(v *revisionView) { v.On = false }

	endedAgo := func(stage string, d time.Duration) func(*revisionView) {
		return func(v *revisionView) {
			v.LastFailed = &state.Heal{
				ID: 7, Kind: state.HealKindRevision, Stage: stage, CreatedAt: epoch.Add(-d - time.Minute), UpdatedAt: epoch.Add(-d),
			}
		}
	}

	ready := revisionView{
		Project: "alpha",
		PR: &state.StandingPR{
			Project: "alpha", URL: "https://github.com/acme/alpha/pull/7", HeadSHA: "h1", State: state.PROpen,
			PushedAt: epoch.Add(-2 * time.Hour),
		},
		Pending: []state.Finding{guidance(1, "G1")},
		Room:    true, On: true, Now: epoch,
	}
	with := func(edits ...func(*revisionView)) revisionView {
		v := ready
		pr := *ready.PR
		v.PR = &pr

		for _, edit := range edits {
			edit(&v)
		}

		return v
	}

	wait := revisionStep{Action: revisionWait}
	send := func(expectHead string, allowed ...string) revisionStep {
		return revisionStep{Action: revisionDispatch, ExpectHead: expectHead, Allowed: allowed}
	}
	end := func(stage, reason string) revisionStep {
		return revisionStep{Action: revisionEnd, Stage: stage, Reason: reason}
	}
	neverReported := revisionStep{
		Action: revisionEnd, Stage: state.StageRevisionFailed, Reason: reasonRevisionUnreported, Unreported: true,
	}

	tests := []struct {
		name string
		v    revisionView
		want revisionStep
	}{
		{"ready, onto the open PR", ready, send("h1")},

		{"in flight and listed", with(open, inFlightFor(time.Hour), func(v *revisionView) { v.Listed = true }), wait},
		{"in flight within the settle grace", with(open, inFlightFor(time.Minute)), wait},
		{"in flight, unlisted past the settle grace", with(open, inFlightFor(settleGrace)), neverReported},
		{"in flight with nothing pending", with(open, inFlightFor(settleGrace), func(v *revisionView) { v.Pending = nil }), neverReported},

		{"pushed", with(open, answered("", outcome(protocol.GuidancePushed))), end(state.StageRevised, "")},
		{"unchanged", with(open, answered("", outcome(protocol.GuidanceUnchanged))), end(state.StageRevised, "")},
		{"stale", with(open, answered("", outcome(protocol.GuidanceStale))), end(state.StageRevisionStale, reasonRevisionStale)},
		{
			"refused", with(open, answered("", outcome(protocol.GuidanceRefused, "a.example.net", "b.example.net"))),
			end(state.StageRevisionRefused, "absent from the repo: a.example.net, b.example.net"),
		},
		{"failed", with(open, answered("symlink escapes the repo", nil)), end(state.StageRevisionFailed, "symlink escapes the repo")},
		{"an answer ends while a push is within the hour", with(open, answered("", outcome(protocol.GuidancePushed)), pushedAgo(time.Minute)), end(state.StageRevised, "")},

		{"nothing pending", with(func(v *revisionView) { v.Pending = nil }), wait},
		{"an unsent open revision with nothing pending", with(open, func(v *revisionView) { v.Pending = nil }), wait},
		{"an unsent open revision is sent again", with(open), send("h1")},

		{"the PR changed outside Sentinel", with(prState(state.PRChangedOutside)), wait},
		{"the PR changed outside Sentinel, pushed long ago", with(prState(state.PRChangedOutside), pushedAgo(48*time.Hour)), wait},

		{"a push 59 minutes ago", with(pushedAgo(59 * time.Minute)), wait},
		{"a push an hour ago to the second", with(pushedAgo(pushEvery)), send("h1")},
		{"a push 61 minutes ago", with(pushedAgo(61 * time.Minute)), send("h1")},
		{"a merged PR pushed within the hour", with(prState(state.PRMerged), pushedAgo(time.Minute)), wait},
		{"a record that never pushed", with(func(v *revisionView) { v.PR.PushedAt = time.Time{} }), send("h1")},
		{
			"three pushes in three hours still leave room for a fourth",
			with(pushedAgo(time.Hour), func(v *revisionView) { v.Pending = append(v.Pending, guidance(2, "G2"), guidance(3, "G3")) }),
			send("h1"),
		},

		{"no worker slot", with(func(v *revisionView) { v.Room = false }), wait},

		{"no PR yet", with(func(v *revisionView) { v.PR = nil }), send("")},
		{"a merged PR starts fresh", with(prState(state.PRMerged)), send("")},
		{"a closed PR starts fresh", with(prState(state.PRClosed)), send("")},
		{"a PR never read starts fresh", with(prState("")), send("")},

		{
			"a refusal a minute ago, pushed two hours ago, retries at once with its allowed",
			with(refusedAgo(time.Minute, "deploy.example.net")), send("h1", "deploy.example.net"),
		},
		{"a refusal older than the last push does not feed allowed", with(refusedAgo(3*time.Hour, "deploy.example.net")), send("h1")},
		{"a refusal with no PR feeds allowed", with(refusedAgo(time.Minute, "x.example.net"), func(v *revisionView) { v.PR = nil }), send("", "x.example.net")},

		{"a revision failed 10 minutes ago", with(endedAgo(state.StageRevisionFailed, 10*time.Minute)), wait},
		{"a revision failed 61 minutes ago", with(endedAgo(state.StageRevisionFailed, 61*time.Minute)), send("h1")},
		{"a revision went stale 10 minutes ago", with(endedAgo(state.StageRevisionStale, 10*time.Minute)), wait},
		{"a failure holds a project with no PR", with(endedAgo(state.StageRevisionFailed, 10*time.Minute), func(v *revisionView) { v.PR = nil }), wait},
		{"friction off sends nothing", with(off), wait},
		{"friction off still ends an unreported revision", with(open, inFlightFor(settleGrace), off), neverReported},
		{"friction off still ends an answered revision", with(open, answered("", outcome(protocol.GuidancePushed)), off), end(state.StageRevised, "")},
		{"friction off leaves an unsent revision", with(open, off), wait},

		{"the open PR's revised allowed list is carried", with(revisedWith("a.example.net")), send("h1", "a.example.net")},
		{"a merged PR's revised allowed list is not", with(revisedWith("a.example.net"), prState(state.PRMerged)), send("")},
		{"a closed PR's revised allowed list is not", with(revisedWith("a.example.net"), prState(state.PRClosed)), send("")},
		{
			"the revised and refused lists are joined",
			with(revisedWith("a", "b"), refusedAgo(time.Minute, "b", "c")), send("h1", "a", "b", "c"),
		},
		{
			"the joined list stops at the agent's limit",
			with(func(v *revisionView) {
				revisedWith(entries("r", 40)...)(v)
				refusedAgo(time.Minute, entries("f", 40)...)(v)
			}),
			send("h1", append(entries("r", 40), entries("f", maxAllowed-40)...)...),
		},
		{
			"a refusal a minute ago after a failure two hours ago retries at once",
			with(endedAgo(state.StageRevisionFailed, 2*time.Hour), refusedAgo(time.Minute, "deploy.example.net")),
			send("h1", "deploy.example.net"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextRevision(tt.v))
		})
	}
}
