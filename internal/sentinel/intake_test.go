package sentinel_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/clock"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/llmchannel"
	opsqlite "github.com/mhersson/contextmatrix/internal/opstore/sqlite"
	"github.com/mhersson/contextmatrix/internal/sentinel"
)

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// flatPricer prices every usage row at the same estimate.
type flatPricer float64

func (f flatPricer) PriceTokens(string, int64, int64, int64, int64) (float64, bool) {
	return float64(f), true
}

type intake struct {
	ctl *sentinel.Controller
	ops *opsqlite.Store
	bus *events.Bus
}

func newIntake(t *testing.T) *intake {
	t.Helper()

	ops, err := opsqlite.Open(filepath.Join(t.TempDir(), "ops.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ops.Close() })

	bus := events.NewBus()
	channel, err := llmchannel.New(context.Background(), llmchannel.Config{}, ops, bus)
	require.NoError(t, err)

	ctl := sentinel.New(sentinel.Config{Store: ops, Channel: channel, Bus: bus, Clock: clock.Fake(now), Pricer: flatPricer(0.02)})

	return &intake{ctl: ctl, ops: ops, bus: bus}
}

// dispatch opens ALPHA-001's heal at stage with a run of kind in flight on
// the endpoint channel, its ceiling 2 USD.
func (in *intake) dispatch(t *testing.T, stage, kind string) {
	t.Helper()

	at := now.Add(-time.Minute)
	_, err := in.ops.OpenHeal(context.Background(), sentinel.Heal{
		Kind: sentinel.HealKindHeal, Project: "alpha", CardID: "ALPHA-001", RunStarted: now.Add(-time.Hour), Stage: stage,
		Dispatch:  sentinel.Dispatch{Kind: kind, Mode: "endpoint", CeilingUSD: 2, At: &at},
		CreatedAt: at, UpdatedAt: at,
	})
	require.NoError(t, err)
}

func (in *intake) heal(t *testing.T) sentinel.Heal {
	t.Helper()

	heals, err := in.ops.CardHeals(context.Background(), "alpha", "ALPHA-001")
	require.NoError(t, err)
	require.Len(t, heals, 1)

	return heals[0]
}

func (in *intake) findings(t *testing.T) []sentinel.Finding {
	t.Helper()

	fs, err := in.ops.CardFindings(context.Background(), "alpha", "ALPHA-001")
	require.NoError(t, err)

	return fs
}

func investigated() protocol.StatusCallbackPayload {
	return protocol.StatusCallbackPayload{
		CardID: "ALPHA-001", Project: "alpha", WorkerStatus: "completed", RunID: "k1",
		StopCause: protocol.StopCauseDone, Kind: protocol.RunKindInvestigate,
		Result: &protocol.RunKindResult{Investigate: &protocol.InvestigateResult{
			Diagnosis: "the verify timeout is too short", Cause: protocol.DiagnosisCard,
			Fix: &protocol.CardFix{Verify: &protocol.VerifyConfig{Command: "go test ./...", TimeoutSeconds: 900}},
			Findings: []protocol.FrictionFinding{{
				Kind: protocol.FindingCardAdvice, Text: "name the slow suite", Evidence: "the run timed out",
			}},
		}},
		Usage: []protocol.RunUsage{{Model: "vendor/model-a", LogTokenUsage: protocol.LogTokenUsage{
			InputTokens: 41210, OutputTokens: 2210, CostUSD: 0.15,
		}}},
	}
}

// sentinelEvents drains ch and returns the sentinel.changed events in it.
func sentinelEvents(ch <-chan events.Event) []events.Event {
	var out []events.Event

	for {
		select {
		case ev := <-ch:
			if ev.Type == events.SentinelChanged {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

func TestRunKindStatusChargesOnceAndNudges(t *testing.T) {
	ctx := context.Background()
	in := newIntake(t)
	in.dispatch(t, sentinel.StageInvestigate, protocol.RunKindInvestigate)

	ch, unsubscribe := in.bus.Subscribe()
	defer unsubscribe()

	require.NoError(t, in.ctl.RunKindStatus(ctx, investigated()))
	assert.True(t, in.ctl.TakeNudgeForTest())

	require.NoError(t, in.ctl.RunKindStatus(ctx, investigated()))
	assert.False(t, in.ctl.TakeNudgeForTest(), "a repeat nudges nothing")

	h := in.heal(t)
	assert.Equal(t, "k1", h.Dispatch.AnsweredRunID)
	assert.InDelta(t, 0.15, h.SpendUSD, 1e-9)
	assert.Equal(t, investigated().Result.Investigate, h.Investigation)
	assert.Len(t, in.findings(t), 1)

	evs := sentinelEvents(ch)
	require.Len(t, evs, 1)
	assert.Equal(t, "alpha", evs[0].Project)
	assert.Equal(t, "ALPHA-001", evs[0].CardID)
}

func TestRunKindStatusAfterParkIsChargedNotApplied(t *testing.T) {
	for _, stage := range []string{sentinel.StageParked, sentinel.StageClosed} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			in := newIntake(t)
			in.dispatch(t, stage, protocol.RunKindInvestigate)

			// The second is a terminal-status retry.
			require.NoError(t, in.ctl.RunKindStatus(ctx, investigated()))
			require.NoError(t, in.ctl.RunKindStatus(ctx, investigated()))

			h := in.heal(t)
			assert.Equal(t, stage, h.Stage)
			assert.Nil(t, h.Change)
			assert.InDelta(t, 0.15, h.SpendUSD, 1e-9)
		})
	}
}

func TestRunKindStatusStoresFindingsOnlyWithFriction(t *testing.T) {
	tests := []struct {
		name     string
		friction bool
		want     int
	}{
		{"friction on", true, 1},
		{"friction off", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			in := newIntake(t)
			in.dispatch(t, sentinel.StageInvestigate, protocol.RunKindInvestigate)

			set := sentinel.DefaultSettings()
			set.Friction = tt.friction
			require.NoError(t, in.ops.PutSentinelSettings(ctx, set, "human:admin"))

			require.NoError(t, in.ctl.RunKindStatus(ctx, investigated()))

			fs := in.findings(t)
			require.Len(t, fs, tt.want)

			for _, f := range fs {
				assert.Equal(t, in.heal(t).ID, f.HealID)
				assert.Equal(t, protocol.FindingCardAdvice, f.Kind)
				assert.Equal(t, "name the slow suite", f.Text)
				assert.Equal(t, "the run timed out", f.Evidence)
			}
		})
	}
}

func TestRunKindStatusRecordsSubscriptionWindows(t *testing.T) {
	ctx := context.Background()
	in := newIntake(t)
	in.dispatch(t, sentinel.StageInvestigate, protocol.RunKindInvestigate)

	fiveHour := protocol.SubscriptionUsage{Window: "five_hour", Utilization: 0.4, Status: "allowed", ResetsAt: now.Add(time.Hour)}
	sevenDay := protocol.SubscriptionUsage{Window: "seven_day", Utilization: 0.9, Status: "allowed_warning", ResetsAt: now.Add(48 * time.Hour)}

	p := investigated()
	p.Usage = []protocol.RunUsage{
		{Model: "claude-sonnet-5-5", LogTokenUsage: protocol.LogTokenUsage{
			InputTokens: 10, CostBasis: protocol.CostBasisSubscription, Subscription: []protocol.SubscriptionUsage{fiveHour},
		}},
		{Model: "claude-haiku-5", LogTokenUsage: protocol.LogTokenUsage{
			InputTokens: 10, CostBasis: protocol.CostBasisSubscription, Subscription: []protocol.SubscriptionUsage{sevenDay},
		}},
	}

	require.NoError(t, in.ctl.RunKindStatus(ctx, p))

	windows, err := in.ops.SubscriptionWindows(ctx)
	require.NoError(t, err)
	assert.Equal(t, []protocol.SubscriptionUsage{fiveHour, sevenDay}, windows)
	assert.InDelta(t, 0.04, in.heal(t).SpendUSD, 1e-9, "subscription rows are charged their estimate")
}

func TestRunKindStatusFailedWithoutUsageChargesCeiling(t *testing.T) {
	tests := []struct {
		name, kind, message, cause string
		wantFailure                string
		wantUSD                    float64
	}{
		{"investigate killed", protocol.RunKindInvestigate, "", protocol.StopCauseKilled, "killed", 2},
		{"investigate with a message", protocol.RunKindInvestigate, "worker image pull failed", protocol.StopCauseInfrastructure, "worker image pull failed", 2},
		{"check charges nothing", protocol.RunKindCheck, "", protocol.StopCauseTimeout, "timeout", 0},
		{"no message or cause", protocol.RunKindInvestigate, "", "", "failed", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			in := newIntake(t)
			in.dispatch(t, sentinel.StageInvestigate, tt.kind)

			require.NoError(t, in.ctl.RunKindStatus(ctx, protocol.StatusCallbackPayload{
				CardID: "ALPHA-001", Project: "alpha", WorkerStatus: "failed", RunID: "k1",
				Message: tt.message, StopCause: tt.cause, Kind: tt.kind,
			}))

			h := in.heal(t)
			assert.Equal(t, "k1", h.Dispatch.AnsweredRunID)
			assert.Equal(t, tt.wantFailure, h.Dispatch.Failure)
			assert.Nil(t, h.Investigation)
			assert.Nil(t, h.Check)
			assert.InDelta(t, tt.wantUSD, h.SpendUSD, 1e-9)
		})
	}
}
