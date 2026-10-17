package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/chat"
)

// liveUpstream is the architect's live route: each connection writes the
// frames of its turn in hello (the last turn repeats), then every frame
// sent on push, and holds the stream open unless end is set for its turn.
type liveUpstream struct {
	hello  [][]string
	status []int // per turn; 0 means 200, the last entry repeats
	end    []bool
	push   chan string

	mu           sync.Mutex
	opened, open int
}

func newLiveUpstream(hello ...[]string) *liveUpstream {
	return &liveUpstream{hello: hello, push: make(chan string)}
}

func (u *liveUpstream) route() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /blueprints/{id}/live": func(w http.ResponseWriter, r *http.Request) {
			u.mu.Lock()
			turn := u.opened
			u.opened++
			u.open++
			u.mu.Unlock()

			defer func() {
				u.mu.Lock()
				u.open--
				u.mu.Unlock()
			}()

			if st := at(u.status, turn); st != 0 && st != http.StatusOK {
				w.WriteHeader(st)

				return
			}

			w.Header().Set("Content-Type", "text/event-stream")

			rc := http.NewResponseController(w)
			_, _ = io.WriteString(w, ": connected\n\n")

			for _, f := range at(u.hello, turn) {
				_, _ = io.WriteString(w, "data: "+f+"\n\n")
			}

			_ = rc.Flush()

			if at(u.end, turn) {
				return
			}

			for {
				select {
				case f := <-u.push:
					_, _ = io.WriteString(w, "data: "+f+"\n\n")
					_ = rc.Flush()
				case <-r.Context().Done():
					return
				}
			}
		},
	}
}

// at returns s[i], the last element past the end, or the zero value.
func at[T any](s []T, i int) T {
	var zero T
	if len(s) == 0 {
		return zero
	}

	return s[min(i, len(s)-1)]
}

func (u *liveUpstream) counts() (opened, open int) {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.opened, u.open
}

func (u *liveUpstream) openCount() int {
	_, open := u.counts()

	return open
}

func (u *liveUpstream) openedCount() int {
	opened, _ := u.counts()

	return opened
}

// openChatStream subscribes to a session's chat SSE like a browser pane;
// cancel is the pane going away.
func openChatStream(t *testing.T, baseURL, id string) (*bufio.Reader, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/chats/"+id+"/stream", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	return bufio.NewReader(resp.Body), cancel
}

// nextBlueprint reads events until a blueprint one and returns its content;
// it asserts the event rides the transcript wire with seq 0.
func nextBlueprint(t *testing.T, r *bufio.Reader) string {
	t.Helper()

	for {
		frame := readSSEFrame(t, r)
		if !strings.HasPrefix(frame, "data: ") {
			continue // comments
		}

		var e struct {
			Seq     *int64 `json:"seq"`
			Content string `json:"content"`
			Kind    string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(frame, "data: "))), &e))

		if e.Kind != chat.EventKindBlueprint {
			continue
		}

		require.NotNil(t, e.Seq)
		assert.Zero(t, *e.Seq)

		return e.Content
	}
}

// pushFrame hands the open upstream its next frame.
func pushFrame(t *testing.T, u *liveUpstream, f string) {
	t.Helper()

	select {
	case u.push <- f:
	case <-time.After(2 * time.Second):
		t.Fatal("no upstream was waiting for a frame")
	}
}

const (
	liveSnap = `{"type":"snapshot","view":{"seq":1}}`
	liveD1   = `{"type":"draft","draft":{"n":1}}`
	liveD2   = `{"type":"draft","draft":{"n":2}}`
	liveD3   = `{"type":"draft","draft":{"n":3}}`
)

// liveFixture starts the chat mux over a fake architect serving u and seeds
// a blueprint; the fixture's short live timings apply unless mutate changes
// them.
func liveFixture(t *testing.T, u *liveUpstream, mutate func(*fixtureOpts)) (srvURL string, id string, fa *fakeArchitect, mux *http.ServeMux) {
	t.Helper()

	fa = newFakeArchitect(t, u.route())
	opts := blueprintFixtureOpts(fa.URL)

	if mutate != nil {
		mutate(&opts)
	}

	mux, mgr := newChatFixture(t, opts)
	bp := seedBlueprint(t, mgr, "human:web")

	cm := httptest.NewServer(mux)
	t.Cleanup(cm.Close)

	return cm.URL, bp.ID, fa, mux
}

func TestBlueprintLive_OneUpstreamForEverySubscriber(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	url, id, _, _ := liveFixture(t, u, nil)

	var readers []*bufio.Reader

	for range 3 {
		r, _ := openChatStream(t, url, id)
		readers = append(readers, r)
	}

	for _, r := range readers {
		assert.JSONEq(t, liveSnap, nextBlueprint(t, r))
	}

	pushFrame(t, u, liveD1)

	for _, r := range readers {
		assert.JSONEq(t, liveD1, nextBlueprint(t, r))
	}

	opened, _ := u.counts()
	assert.Equal(t, 1, opened)
}

func TestBlueprintLive_LastSubscriberLeavingClosesTheUpstream(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	url, id, _, _ := liveFixture(t, u, nil)

	a, cancelA := openChatStream(t, url, id)
	b, cancelB := openChatStream(t, url, id)
	nextBlueprint(t, a)
	nextBlueprint(t, b)

	cancelA()
	assert.Never(t, func() bool { return u.openCount() == 0 }, 500*time.Millisecond, 20*time.Millisecond)

	cancelB()
	assert.Eventually(t, func() bool { return u.openCount() == 0 }, 2*time.Second, 10*time.Millisecond)
}

func TestBlueprintLive_ReturnWithinTheLingerKeepsTheUpstream(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	url, id, _, _ := liveFixture(t, u, func(o *fixtureOpts) { o.liveLinger = 2 * time.Second })

	a, cancelA := openChatStream(t, url, id)
	assert.JSONEq(t, liveSnap, nextBlueprint(t, a))
	pushFrame(t, u, liveD1)
	assert.JSONEq(t, liveD1, nextBlueprint(t, a))

	cancelA()

	b, _ := openChatStream(t, url, id)
	assert.JSONEq(t, liveSnap, nextBlueprint(t, b))
	assert.JSONEq(t, liveD1, nextBlueprint(t, b))

	opened, _ := u.counts()
	assert.Equal(t, 1, opened)
}

func TestBlueprintLive_LateSubscriberGetsTheSnapshotAndDraftsFirst(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	url, id, _, _ := liveFixture(t, u, nil)

	a, _ := openChatStream(t, url, id)
	assert.JSONEq(t, liveSnap, nextBlueprint(t, a))

	for _, d := range []string{liveD1, liveD2} {
		pushFrame(t, u, d)
		assert.JSONEq(t, d, nextBlueprint(t, a))
	}

	b, _ := openChatStream(t, url, id)
	for _, want := range []string{liveSnap, liveD1, liveD2} {
		assert.JSONEq(t, want, nextBlueprint(t, b))
	}

	pushFrame(t, u, liveD3)
	assert.JSONEq(t, liveD3, nextBlueprint(t, a))
	assert.JSONEq(t, liveD3, nextBlueprint(t, b))
}

func TestBlueprintLive_FramesStayOutOfTheTranscript(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	url, id, _, mux := liveFixture(t, u, nil)

	a, _ := openChatStream(t, url, id)
	nextBlueprint(t, a)
	pushFrame(t, u, liveD1)
	nextBlueprint(t, a)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, jsonReq(t, http.MethodGet, "/api/chats/"+id+"/messages", ""))
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Messages []json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Messages)
}

func TestBlueprintLive_NotServedStopsReconnecting(t *testing.T) {
	u := newLiveUpstream()
	u.status = []int{http.StatusNotFound}
	url, id, _, _ := liveFixture(t, u, nil)

	_, cancelA := openChatStream(t, url, id)

	assert.Never(t, func() bool { return u.openedCount() > 1 }, 300*time.Millisecond, 10*time.Millisecond)

	cancelA()
	time.Sleep(200 * time.Millisecond) // past the linger

	openChatStream(t, url, id)
	assert.Eventually(t, func() bool { return u.openedCount() == 2 }, 2*time.Second, 10*time.Millisecond)
}

func TestBlueprintLive_ReconnectsAndTheNewSnapshotWins(t *testing.T) {
	snap2 := `{"type":"snapshot","view":{"seq":2}}`
	u := newLiveUpstream(nil, []string{liveSnap, liveD1}, []string{snap2})
	u.status = []int{500, 0, 0}
	u.end = []bool{false, true, false}
	url, id, _, _ := liveFixture(t, u, nil)

	a, _ := openChatStream(t, url, id)
	assert.JSONEq(t, liveSnap, nextBlueprint(t, a))
	assert.JSONEq(t, liveD1, nextBlueprint(t, a))
	assert.JSONEq(t, snap2, nextBlueprint(t, a))

	b, _ := openChatStream(t, url, id)
	assert.JSONEq(t, snap2, nextBlueprint(t, b))

	opened, _ := u.counts()
	assert.Equal(t, 3, opened)
}

func TestBlueprintLive_LargeSnapshotPassesWhole(t *testing.T) {
	big := `{"type":"snapshot","draft":"` + strings.Repeat("é", 150<<10) + `"}`
	u := newLiveUpstream([]string{big})
	url, id, _, _ := liveFixture(t, u, nil)

	a, _ := openChatStream(t, url, id)
	assert.Equal(t, big, nextBlueprint(t, a))
}

func TestBlueprintLive_ChatSessionsNeverOpenAnUpstream(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	fa := newFakeArchitect(t, u.route())
	mux, mgr := newChatFixture(t, blueprintFixtureOpts(fa.URL))
	sess := seedSession(t, mgr, "human:web")

	cm := httptest.NewServer(mux)
	t.Cleanup(cm.Close)

	openChatStream(t, cm.URL, sess.ID)
	assert.Never(t, func() bool { return len(fa.seen()) > 0 }, 200*time.Millisecond, 10*time.Millisecond)
}

func TestRouter_WiresTheBlueprintRelay(t *testing.T) {
	u := newLiveUpstream([]string{liveSnap})
	u.end = []bool{true}
	fa := newFakeArchitect(t, u.route())
	opts := blueprintFixtureOpts(fa.URL)
	_, mgr := newChatFixture(t, opts)
	bp := seedBlueprint(t, mgr, "human:web")

	srv := httptest.NewServer(NewRouter(RouterConfig{
		ChatManager: mgr, ChatHub: chat.NewSSEHub(8), ArchitectBackendCfg: opts.architectBackendCfg,
	}))
	t.Cleanup(srv.Close)

	r, _ := openChatStream(t, srv.URL, bp.ID)
	assert.JSONEq(t, liveSnap, nextBlueprint(t, r))
}

func TestReadLiveFrames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "data: a\n\n", []string{"a"}},
		{"no space", "data:a\n\n", []string{"a"}},
		{"comment skipped", ": c\n\ndata: a\n\n", []string{"a"}},
		{"other fields skipped", "event: x\nid: 1\ndata: a\n\n", []string{"a"}},
		{"multi-line data", "data: a\ndata: b\n\n", []string{"a\nb"}},
		{"crlf", "data: a\r\n\r\n", []string{"a"}},
		{"cut off", "data: a", nil},
		{"two frames", "data: a\n\ndata: b\n\n", []string{"a", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string

			require.NoError(t, readLiveFrames(strings.NewReader(tt.in), func(f string) { got = append(got, f) }))
			assert.Equal(t, tt.want, got)
		})
	}
}
