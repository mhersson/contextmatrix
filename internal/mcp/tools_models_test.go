package mcp

import (
	"context"
	"testing"
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
