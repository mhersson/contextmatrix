package chat_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
)

const (
	snap1 = `{"type":"snapshot","view":{"seq":1}}`
	snap2 = `{"type":"snapshot","view":{"seq":2}}`
	d1    = `{"type":"draft","draft":{"offset":0,"text":"a"}}`
	d2    = `{"type":"draft","draft":{"offset":1,"text":"b"}}`
)

// drain returns what ch holds right now without waiting.
func drain(ch <-chan chat.SSEEvent) []chat.SSEEvent {
	var out []chat.SSEEvent

	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

func contents(events []chat.SSEEvent, dataKind string) []string {
	var out []string

	for _, e := range events {
		if e.DataKind == dataKind {
			out = append(out, e.Content)
		}
	}

	return out
}

// replayOf returns the blueprint contents a fresh subscriber is replayed.
func replayOf(t *testing.T, hub *chat.SSEHub, sessionID string) []string {
	t.Helper()

	_, replay, err := hub.Subscribe(sessionID, 0)
	require.NoError(t, err)

	return contents(replay, chat.EventKindBlueprint)
}

type bpFrame struct {
	typ, frame string
}

func TestBlueprintFrames_ReachEverySubscriberAndStayOutOfTheRing(t *testing.T) {
	hub := chat.NewSSEHub(4)

	a, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)
	b, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	for seq := int64(1); seq <= 3; seq++ {
		hub.Publish("S1", chat.SSEEvent{Seq: seq, Role: chat.RoleAssistantText, Content: "m"})
	}

	for range 10 {
		hub.PublishBlueprint("S1", 1, "draft", d1)
	}

	hub.PublishBlueprint("S1", 1, "snapshot", snap1)

	for _, ch := range []<-chan chat.SSEEvent{a, b} {
		got := drain(ch)
		require.NotEmpty(t, got)
		last := got[len(got)-1]
		assert.Equal(t, chat.EventKindBlueprint, last.DataKind)
		assert.Equal(t, []string{snap1}, []string{last.Content}, "the frame is relayed verbatim")
		assert.Zero(t, last.Seq)
	}

	_, replay, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	var seqs []int64

	for _, e := range replay {
		if e.DataKind == "" {
			seqs = append(seqs, e.Seq)
		}
	}

	assert.Equal(t, []int64{1, 2, 3}, seqs, "blueprint frames never enter or evict the ring")
	assert.Equal(t, []string{snap1}, contents(replay, chat.EventKindBlueprint))
}

func TestBlueprintCache_NewSubscriberStartsWithTheSnapshot(t *testing.T) {
	tests := []struct {
		name string
		seq  []bpFrame
		want []string
	}{
		{"draft alone is not cached", []bpFrame{{"draft", d1}}, nil},
		{"snapshot then drafts", []bpFrame{{"snapshot", snap1}, {"draft", d1}, {"draft", d2}}, []string{snap1, d1, d2}},
		{"newer snapshot resets drafts", []bpFrame{{"snapshot", snap1}, {"draft", d1}, {"snapshot", snap2}}, []string{snap2}},
		{
			"other types are not cached",
			[]bpFrame{{"snapshot", snap1}, {"artifact_stored", `{"type":"artifact_stored"}`}, {"draft", d1}},
			[]string{snap1, d1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := chat.NewSSEHub(4)
			_, _, err := hub.Subscribe("S1", 0)
			require.NoError(t, err)

			for _, f := range tt.seq {
				hub.PublishBlueprint("S1", 1, f.typ, f.frame)
			}

			assert.Equal(t, tt.want, replayOf(t, hub, "S1"))
		})
	}
}

func TestBlueprintFrames_ArtifactStoredIsDeliveredLive(t *testing.T) {
	hub := chat.NewSSEHub(4)
	ch, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	const stored = `{"type":"artifact_stored"}`

	hub.PublishBlueprint("S1", 1, "artifact_stored", stored)

	assert.Equal(t, []string{stored}, contents(drain(ch), chat.EventKindBlueprint))
}

func TestBlueprintCache_DraftOverflowKeepsTheSnapshot(t *testing.T) {
	hub := chat.NewSSEHub(4)
	ch, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	big := `{"type":"draft","draft":{"text":"` + strings.Repeat("x", 100<<10) + `"}}`

	hub.PublishBlueprint("S1", 1, "snapshot", snap1)

	for range 3 {
		hub.PublishBlueprint("S1", 1, "draft", big)
	}

	assert.Equal(t, []string{snap1}, replayOf(t, hub, "S1"))

	hub.PublishBlueprint("S1", 1, "draft", d2)
	assert.Equal(t, []string{snap1}, replayOf(t, hub, "S1"), "no caching until the next snapshot")

	hub.PublishBlueprint("S1", 1, "snapshot", snap2)
	hub.PublishBlueprint("S1", 1, "draft", d1)
	assert.Equal(t, []string{snap2, d1}, replayOf(t, hub, "S1"))

	got := contents(drain(ch), chat.EventKindBlueprint)
	require.GreaterOrEqual(t, len(got), 2)
	assert.Equal(t, []string{snap1}, got[:1])
	assert.Equal(t, big, got[1], "live subscribers still get drafts the cache dropped")
}

func TestBlueprintFrames_NeverCrowdOutTranscriptEvents(t *testing.T) {
	hub := chat.NewSSEHub(4)
	ch, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	hub.PublishBlueprint("S1", 1, "snapshot", snap1)

	for range 63 {
		hub.PublishBlueprint("S1", 1, "draft", d1)
	}

	hub.Publish("S1", chat.SSEEvent{Seq: 1, Content: "kept"})

	got := drain(ch)

	assert.Len(t, contents(got, chat.EventKindBlueprint), 32, "blueprint frames stop at half the buffer")

	var kept bool

	for _, e := range got {
		if e.DataKind == "" && e.Seq == 1 {
			kept = true
		}
	}

	assert.True(t, kept, "the transcript event still fits")
}

func TestBlueprintFrames_StaleEpochNeverTouchesTheCache(t *testing.T) {
	hub := chat.NewSSEHub(4)
	first, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)

	hub.PublishBlueprint("S1", 2, "snapshot", snap2)
	hub.PublishBlueprint("S1", 1, "snapshot", snap1)

	assert.Equal(t, []string{snap2}, replayOf(t, hub, "S1"))
	assert.Equal(t, []string{snap2}, contents(drain(first), chat.EventKindBlueprint), "stale frame is not fanned out")

	hub.ResetBlueprint("S1", 1)
	assert.Equal(t, []string{snap2}, replayOf(t, hub, "S1"))

	hub.ResetBlueprint("S1", 2)
	assert.Nil(t, replayOf(t, hub, "S1"))

	hub.PublishBlueprint("S1", 2, "snapshot", snap2)
	hub.PublishBlueprint("S1", 3, "draft", d1)
	assert.Nil(t, replayOf(t, hub, "S1"), "a newer epoch resets the cache and a draft alone is not cached")
}

func TestBlueprintFrames_SkipSessionsNobodySubscribed(t *testing.T) {
	hub := chat.NewSSEHub(4)

	hub.PublishBlueprint("never", 1, "snapshot", snap1)
	assert.Nil(t, replayOf(t, hub, "never"))

	_, _, err := hub.Subscribe("S1", 0)
	require.NoError(t, err)
	hub.Drop("S1")

	hub.PublishBlueprint("S1", 1, "snapshot", snap1)
	assert.Nil(t, replayOf(t, hub, "S1"), "a relay outliving a deleted session never recreates its hub")
}
