package chat_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
)

func TestSSEHub_PublishFansOut(t *testing.T) {
	hub := chat.NewSSEHub(128)
	sub1, _, _ := hub.Subscribe("S1", 0)
	sub2, _, _ := hub.Subscribe("S1", 0)

	t.Cleanup(func() { hub.Unsubscribe("S1", sub1); hub.Unsubscribe("S1", sub2) })

	hub.Publish("S1", chat.SSEEvent{Seq: 1, Role: chat.RoleUser, Content: "{}"})

	got1, ok := readSSEWithTimeout(t, sub1, 500*time.Millisecond)
	require.True(t, ok, "sub1 must receive event")
	assert.Equal(t, int64(1), got1.Seq)
	got2, ok := readSSEWithTimeout(t, sub2, 500*time.Millisecond)
	require.True(t, ok, "sub2 must receive event")
	assert.Equal(t, int64(1), got2.Seq)
}

func TestSSEHub_ReplaysFromSinceSeq(t *testing.T) {
	hub := chat.NewSSEHub(128)
	hub.Publish("S1", chat.SSEEvent{Seq: 1, Content: "a"})
	hub.Publish("S1", chat.SSEEvent{Seq: 2, Content: "b"})
	hub.Publish("S1", chat.SSEEvent{Seq: 3, Content: "c"})

	sub, replay, _ := hub.Subscribe("S1", 1)

	t.Cleanup(func() { hub.Unsubscribe("S1", sub) })
	require.Len(t, replay, 2, "replay should include seq 2, 3 (since_seq=1)")
	assert.Equal(t, int64(2), replay[0].Seq)
	assert.Equal(t, int64(3), replay[1].Seq)
}

func TestSSEHub_RingBufferEvicts(t *testing.T) {
	hub := chat.NewSSEHub(2)
	hub.Publish("S1", chat.SSEEvent{Seq: 1})
	hub.Publish("S1", chat.SSEEvent{Seq: 2})
	hub.Publish("S1", chat.SSEEvent{Seq: 3})

	_, replay, _ := hub.Subscribe("S1", 0)
	require.Len(t, replay, 2)
	assert.Equal(t, int64(2), replay[0].Seq)
	assert.Equal(t, int64(3), replay[1].Seq)
}

func TestSSEHub_PerSessionIsolation(t *testing.T) {
	hub := chat.NewSSEHub(128)
	subA, _, _ := hub.Subscribe("A", 0)
	subB, _, _ := hub.Subscribe("B", 0)

	t.Cleanup(func() { hub.Unsubscribe("A", subA); hub.Unsubscribe("B", subB) })

	hub.Publish("A", chat.SSEEvent{Seq: 1, Content: "from-A"})

	_, ok := readSSEWithTimeout(t, subA, 200*time.Millisecond)
	assert.True(t, ok, "A subscriber must receive A's events")
	_, ok2 := readSSEWithTimeout(t, subB, 100*time.Millisecond)
	assert.False(t, ok2, "B subscriber must not receive A's events")
}

func TestSSEHub_SubscriberCap(t *testing.T) {
	t.Parallel()

	hub := chat.NewSSEHub(128)

	var chs []<-chan chat.SSEEvent

	for i := range 32 {
		ch, _, err := hub.Subscribe("cap-session", 0)
		require.NoError(t, err, "subscriber %d should succeed", i)

		chs = append(chs, ch)
	}

	t.Cleanup(func() {
		for _, ch := range chs {
			hub.Unsubscribe("cap-session", ch)
		}
	})

	_, _, err := hub.Subscribe("cap-session", 0)
	require.Error(t, err, "33rd subscriber must be rejected")
	require.Contains(t, err.Error(), "subscriber cap")
}

func readSSEWithTimeout(t *testing.T, ch <-chan chat.SSEEvent, d time.Duration) (chat.SSEEvent, bool) {
	t.Helper()

	select {
	case e := <-ch:
		return e, true
	case <-time.After(d):
		return chat.SSEEvent{}, false
	}
}

func TestSSEHub_LastUnsubscribe_FiresCallback(t *testing.T) {
	hub := chat.NewSSEHub(64)

	var fired bool

	hub.OnLastUnsubscribe = func(sessionID string) {
		if sessionID == "S1" {
			fired = true
		}
	}
	sub, _, _ := hub.Subscribe("S1", 0)
	hub.Unsubscribe("S1", sub)
	assert.True(t, fired, "callback should fire on last unsubscribe")
}

func TestSSEHub_AdditionalSubsDontFire(t *testing.T) {
	hub := chat.NewSSEHub(64)

	var fires int

	hub.OnLastUnsubscribe = func(string) { fires++ }
	sub1, _, _ := hub.Subscribe("S1", 0)
	sub2, _, _ := hub.Subscribe("S1", 0)
	hub.Unsubscribe("S1", sub1)
	assert.Equal(t, 0, fires, "callback fires only on last subscriber leaving")
	hub.Unsubscribe("S1", sub2)
	assert.Equal(t, 1, fires)
}

func TestSSEHub_OnSubscribe_Fires(t *testing.T) {
	hub := chat.NewSSEHub(64)

	var got string

	hub.OnSubscribe = func(sessionID string) { got = sessionID }
	sub, _, _ := hub.Subscribe("S7", 0)

	t.Cleanup(func() { hub.Unsubscribe("S7", sub) })
	assert.Equal(t, "S7", got)
}
