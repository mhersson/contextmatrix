package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/mhersson/contextmatrix/internal/metrics"
)

type fakeBlacklistWriter struct {
	slug, reason, card, by string
	called                 bool
}

func (f *fakeBlacklistWriter) RecordIncapableModel(_ context.Context, slug, reason, card, by string) error {
	f.slug, f.reason, f.card, f.by, f.called = slug, reason, card, by, true

	return nil
}

func TestReportIncapableModelHandler(t *testing.T) {
	w := &fakeBlacklistWriter{}

	_, _, err := reportIncapableModelHandler(w)(context.Background(), nil, reportIncapableModelInput{
		ModelSlug: "bad/m", Reason: "parse failures", SampleCardID: "CM-1", AgentID: "agent:x",
	})
	if err != nil {
		t.Fatal(err)
	}

	if !w.called || w.slug != "bad/m" || w.reason != "parse failures" {
		t.Errorf("not recorded: %+v", w)
	}
}

func TestReportIncapableModelHandlerIncrementsMetric(t *testing.T) {
	w := &fakeBlacklistWriter{}

	const slug = "bad/metrics-model"

	base := testutil.ToFloat64(metrics.ModelBlacklistsTotal.WithLabelValues(slug))

	_, _, err := reportIncapableModelHandler(w)(context.Background(), nil, reportIncapableModelInput{
		ModelSlug: slug, Reason: "parse failures", AgentID: "agent:x",
	})
	if err != nil {
		t.Fatal(err)
	}

	after := testutil.ToFloat64(metrics.ModelBlacklistsTotal.WithLabelValues(slug))
	if after != base+1 {
		t.Errorf("ModelBlacklistsTotal = %v, want %v", after, base+1)
	}
}

func TestReportIncapableModelHandlerNoMetricOnWriteError(t *testing.T) {
	const slug = "bad/metrics-model-err"

	base := testutil.ToFloat64(metrics.ModelBlacklistsTotal.WithLabelValues(slug))

	_, _, err := reportIncapableModelHandler(&errBlacklistWriter{})(context.Background(), nil, reportIncapableModelInput{
		ModelSlug: slug, Reason: "parse failures", AgentID: "agent:x",
	})
	if err == nil {
		t.Fatal("expected error from failing writer")
	}

	after := testutil.ToFloat64(metrics.ModelBlacklistsTotal.WithLabelValues(slug))
	if after != base {
		t.Errorf("ModelBlacklistsTotal = %v, want unchanged %v", after, base)
	}
}

type errBlacklistWriter struct{}

func (errBlacklistWriter) RecordIncapableModel(_ context.Context, _, _, _, _ string) error {
	return errors.New("db closed")
}
