package service

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

func histogramSampleSum(t *testing.T, vec *prometheus.HistogramVec, lvs ...string) float64 {
	t.Helper()

	h, err := vec.GetMetricWithLabelValues(lvs...)
	require.NoError(t, err)

	m := &dto.Metric{}
	require.NoError(t, h.(prometheus.Metric).Write(m))

	return m.GetHistogram().GetSampleSum()
}

func strPtr(s string) *string { return &s }

func TestPhaseDurationMetric(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Phase timing", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	planCountBase := histogramSampleCount(t, metrics.PhaseDuration, "test-project", "plan")
	planSumBase := histogramSampleSum(t, metrics.PhaseDuration, "test-project", "plan")
	execCountBase := histogramSampleCount(t, metrics.PhaseDuration, "test-project", "execute")
	doneCountBase := histogramSampleCount(t, metrics.PhaseDuration, "test-project", "done")

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: strPtr("plan")})
	require.NoError(t, err)

	// Entering a phase starts the timer but observes nothing yet.
	assert.Equal(t, planCountBase, histogramSampleCount(t, metrics.PhaseDuration, "test-project", "plan"))

	fake.Advance(10 * time.Minute)

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: strPtr("execute")})
	require.NoError(t, err)

	assert.Equal(t, planCountBase+1, histogramSampleCount(t, metrics.PhaseDuration, "test-project", "plan"))
	assert.InDelta(t, planSumBase+600, histogramSampleSum(t, metrics.PhaseDuration, "test-project", "plan"), 1e-6)

	// Patching an unrelated field must not observe the running phase.
	fake.Advance(time.Minute)

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Priority: strPtr("high")})
	require.NoError(t, err)

	assert.Equal(t, execCountBase, histogramSampleCount(t, metrics.PhaseDuration, "test-project", "execute"))

	fake.Advance(4 * time.Minute)

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: strPtr("done")})
	require.NoError(t, err)

	assert.Equal(t, execCountBase+1, histogramSampleCount(t, metrics.PhaseDuration, "test-project", "execute"))

	// done ends timing: moving off it later observes no "done" duration.
	fake.Advance(time.Minute)

	_, err = svc.PatchCard(ctx, "test-project", card.ID, PatchCardInput{Phase: strPtr("plan")})
	require.NoError(t, err)

	assert.Equal(t, doneCountBase, histogramSampleCount(t, metrics.PhaseDuration, "test-project", "done"))
}

func TestRunLifecycleMetricsCompleted(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Run completed", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	runsBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "completed", "normal"))
	durCountBase := histogramSampleCount(t, metrics.CardRunDuration, "test-project", "completed", "normal")
	durSumBase := histogramSampleSum(t, metrics.CardRunDuration, "test-project", "completed", "normal")
	agentsCountBase := histogramSampleCount(t, metrics.RunAgents, "normal")
	failedBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "failed", "normal"))

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(30 * time.Minute)

	_, err = svc.TransitionTo(ctx, "test-project", card.ID, "in_progress")
	require.NoError(t, err)
	_, err = svc.TransitionTo(ctx, "test-project", card.ID, "done")
	require.NoError(t, err)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	assert.InDelta(t, runsBase+1,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "completed", "normal")), 1e-9)
	assert.Equal(t, durCountBase+1, histogramSampleCount(t, metrics.CardRunDuration, "test-project", "completed", "normal"))
	assert.InDelta(t, durSumBase+1800, histogramSampleSum(t, metrics.CardRunDuration, "test-project", "completed", "normal"), 1e-6)
	assert.Equal(t, agentsCountBase+1, histogramSampleCount(t, metrics.RunAgents, "normal"))

	// A late failed callback after the release must not double count: the
	// claim is already cleared, so no run is open.
	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "failed", "late cleanup")
	require.NoError(t, err)

	assert.InDelta(t, failedBase,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "failed", "normal")), 1e-9)
}

func TestRunLifecycleMetricsReleasedAndReclaim(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Run released", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	releasedBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "released", "normal"))
	durSumBase := histogramSampleSum(t, metrics.CardRunDuration, "test-project", "released", "normal")

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(10 * time.Minute)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	// A re-claim starts a fresh run with its own duration.
	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(5 * time.Minute)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	assert.InDelta(t, releasedBase+2,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "released", "normal")), 1e-9)
	assert.InDelta(t, durSumBase+600+300,
		histogramSampleSum(t, metrics.CardRunDuration, "test-project", "released", "normal"), 1e-6)
}

func TestRunLifecycleMetricsStalled(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Run stalled", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	stalledBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "stalled", "normal"))

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "stale-agent")
	require.NoError(t, err)

	fake.Advance(10 * time.Millisecond)

	require.NoError(t, svc.processStalled(ctx))

	assert.InDelta(t, stalledBase+1,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "stalled", "normal")), 1e-9)
}

func TestRunLifecycleMetricsFailedCallback(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Run failed", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	failedBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "failed", "normal"))
	durSumBase := histogramSampleSum(t, metrics.CardRunDuration, "test-project", "failed", "normal")

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(7 * time.Minute)

	_, err = svc.UpdateWorkerStatus(ctx, "test-project", card.ID, "failed", "container crashed")
	require.NoError(t, err)

	assert.InDelta(t, failedBase+1,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "failed", "normal")), 1e-9)
	assert.InDelta(t, durSumBase+420,
		histogramSampleSum(t, metrics.CardRunDuration, "test-project", "failed", "normal"), 1e-6)
}

func TestRunLifecycleMetricsSubtaskExcluded(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	parent, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Parent card", Type: "task", Priority: "medium",
	})
	require.NoError(t, err)

	sub, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Subtask card", Type: "task", Priority: "medium", Parent: parent.ID,
	})
	require.NoError(t, err)

	seriesBase := testutil.CollectAndCount(metrics.CardRunsTotal)

	_, err = svc.ClaimCard(ctx, "test-project", sub.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(time.Minute)

	_, err = svc.ReleaseCard(ctx, "test-project", sub.ID, "cmx-agent-1")
	require.NoError(t, err)

	assert.Equal(t, seriesBase, testutil.CollectAndCount(metrics.CardRunsTotal),
		"subtask claims must not produce run metrics")
}

func TestRunLifecycleMetricsRunModeAndAgents(t *testing.T) {
	svc, fake, cleanup := newStalledTestService(t)
	defer cleanup()

	ctx := context.Background()

	card, err := svc.CreateCard(ctx, "test-project", CreateCardInput{
		Title: "Mob run", Type: "task", Priority: "medium",
		MobParticipants: 3, MobGuests: []string{"guest-a"},
	})
	require.NoError(t, err)

	mobRunsBase := testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "released", "mob"))
	mobAgentsCountBase := histogramSampleCount(t, metrics.RunAgents, "mob")
	mobAgentsSumBase := histogramSampleSum(t, metrics.RunAgents, "mob")

	_, err = svc.ClaimCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	fake.Advance(time.Minute)

	_, err = svc.ReleaseCard(ctx, "test-project", card.ID, "cmx-agent-1")
	require.NoError(t, err)

	assert.InDelta(t, mobRunsBase+1,
		testutil.ToFloat64(metrics.CardRunsTotal.WithLabelValues("test-project", "released", "mob")), 1e-9)
	assert.Equal(t, mobAgentsCountBase+1, histogramSampleCount(t, metrics.RunAgents, "mob"))
	assert.InDelta(t, mobAgentsSumBase+4, histogramSampleSum(t, metrics.RunAgents, "mob"), 1e-9,
		"mob agents = participants + guests")
}

func TestReleaseOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state string
		want  string
	}{
		{"done", "completed"},
		{"review", "review_parked"},
		{"in_progress", "released"},
		{"todo", "released"},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, releaseOutcome(tt.state))
		})
	}
}
