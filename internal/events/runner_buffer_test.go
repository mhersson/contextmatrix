package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunnerEventBufferAppendAndReplay(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 24*time.Hour)

	buf.Append("card1", RunnerEvent{Type: "chat_input", Data: "hello"})
	buf.Append("card1", RunnerEvent{Type: "stop"})

	events := buf.Since("card1", 0)
	require.Len(t, events, 2)
	require.Equal(t, uint64(1), events[0].EventID)
	require.Equal(t, uint64(2), events[1].EventID)

	events = buf.Since("card1", 1)
	require.Len(t, events, 1)
	require.Equal(t, "stop", events[0].Type)
}

func TestRunnerEventBufferCardIsolation(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 24*time.Hour)
	buf.Append("card1", RunnerEvent{Type: "a"})
	buf.Append("card2", RunnerEvent{Type: "b"})
	require.Len(t, buf.Since("card1", 0), 1)
	require.Len(t, buf.Since("card2", 0), 1)
	require.Equal(t, "a", buf.Since("card1", 0)[0].Type)
}

func TestRunnerEventBufferEvictsByCount(t *testing.T) {
	buf := NewRunnerEventBuffer(3, 24*time.Hour)
	for i := 0; i < 5; i++ {
		buf.Append("c", RunnerEvent{Type: "e"})
	}

	events := buf.Since("c", 0)
	require.Len(t, events, 3, "buffer kept newest 3")
	require.Equal(t, uint64(3), events[0].EventID, "oldest kept is event 3")
	require.Equal(t, uint64(5), events[2].EventID)
}

func TestRunnerEventBufferEvictsByAge(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 50*time.Millisecond)
	buf.Append("c", RunnerEvent{Type: "old"})
	time.Sleep(80 * time.Millisecond)
	buf.Append("c", RunnerEvent{Type: "new"})

	events := buf.Since("c", 0)
	require.Len(t, events, 1, "old event evicted by age")
	require.Equal(t, "new", events[0].Type)
}

func TestRunnerEventBufferReplayMissedEventsBeforeWindow(t *testing.T) {
	// After eviction, Since(0) returns only what's still in the buffer.
	// The earliest EventID returned is > 0, telling subscribers their
	// requested window has been (partially) lost.
	buf := NewRunnerEventBuffer(2, 24*time.Hour)
	buf.Append("c", RunnerEvent{Type: "a"}) // id 1, evicted
	buf.Append("c", RunnerEvent{Type: "b"}) // id 2
	buf.Append("c", RunnerEvent{Type: "c"}) // id 3, evicts id 1

	events := buf.Since("c", 0)
	require.Len(t, events, 2)
	require.Equal(t, uint64(2), events[0].EventID, "earliest kept event is 2 — caller can detect lost messages by event_id > requested+1")
	require.Equal(t, uint64(3), events[1].EventID)
}

func TestRunnerEventBufferSubscribe(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 24*time.Hour)

	ch, cancel := buf.Subscribe("card1")
	defer cancel()

	go buf.Append("card1", RunnerEvent{Type: "x"})

	select {
	case ev := <-ch:
		require.Equal(t, "x", ev.Type)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestRunnerEventBufferSubscribeCancel(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 24*time.Hour)
	ch, cancel := buf.Subscribe("card1")
	cancel()

	// After cancel, channel is closed.
	_, ok := <-ch
	require.False(t, ok, "channel should be closed after cancel")
}

func TestRunnerEventBufferAppendAssignsTimestamp(t *testing.T) {
	buf := NewRunnerEventBuffer(100, 24*time.Hour)
	before := time.Now()
	ev := buf.Append("c", RunnerEvent{Type: "x"})
	after := time.Now()
	require.True(t, !ev.At.Before(before) && !ev.At.After(after), "Append assigns At from time.Now()")
	require.Equal(t, uint64(1), ev.EventID)
}
