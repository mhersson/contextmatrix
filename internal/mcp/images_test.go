package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mhersson/contextmatrix/internal/board"
	"github.com/mhersson/contextmatrix/internal/events"
	"github.com/mhersson/contextmatrix/internal/gitops"
	"github.com/mhersson/contextmatrix/internal/images"
	"github.com/mhersson/contextmatrix/internal/lock"
	"github.com/mhersson/contextmatrix/internal/service"
	"github.com/mhersson/contextmatrix/internal/storage"
)

func TestExtractCMImageIDs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "relative URL",
			body: "see ![shot](/api/images/aabbccddeeff0011)",
			want: []string{"aabbccddeeff0011"},
		},
		{
			name: "absolute URL with host",
			body: "shot: ![](http://localhost:8080/api/images/0123456789abcdef)",
			want: []string{"0123456789abcdef"},
		},
		{
			name: "https absolute URL",
			body: "![alt text](https://cm.example/api/images/0123456789abcdef)",
			want: []string{"0123456789abcdef"},
		},
		{
			name: "mixed cm and external image refs",
			body: "![ours](/api/images/aaaaaaaaaaaaaaaa) and ![theirs](https://imgur.com/foo.png)",
			want: []string{"aaaaaaaaaaaaaaaa"},
		},
		{
			name: "dedup",
			body: "![](/api/images/abcdef0123456789) ![](/api/images/abcdef0123456789)",
			want: []string{"abcdef0123456789"},
		},
		{
			name: "non-hex IDs rejected",
			body: "![](/api/images/zzzzzzzzzzzzzzzz)",
			want: nil,
		},
		{
			name: "wrong length rejected",
			body: "![](/api/images/abc) ![](/api/images/abcdef012345678900)",
			want: nil,
		},
		{
			name: "no images",
			body: "plain markdown text [link](https://example.com)",
			want: nil,
		},
		{
			name: "trailing path segment rejected",
			body: "![](/api/images/aabbccddeeff0011/extra)",
			want: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractCMImageIDs(tc.body)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExtractCMImageIDs_TenCap(t *testing.T) {
	var b strings.Builder

	// 12 unique IDs — function should cap at 10.
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "![](/api/images/%016x)\n", i)
	}

	ids := extractCMImageIDs(b.String())
	assert.Len(t, ids, maxAttachedImages)
}

// mcpImageEnv extends the standard MCP test env with an image store.
type mcpImageEnv struct {
	session *mcp.ClientSession
	svc     *service.CardService
	store   images.Store
	cancel  context.CancelFunc
}

func setupMCPImages(t *testing.T) *mcpImageEnv {
	t.Helper()

	tmpDir := t.TempDir()
	boardsDir := filepath.Join(tmpDir, "boards")
	require.NoError(t, os.MkdirAll(boardsDir, 0o755))

	projectDir := filepath.Join(boardsDir, "test-project")
	require.NoError(t, os.MkdirAll(filepath.Join(projectDir, "tasks"), 0o755))
	require.NoError(t, board.SaveProjectConfig(projectDir, testProjectConfig()))

	store, err := storage.NewFilesystemStore(boardsDir)
	require.NoError(t, err)

	gitMgr, err := gitops.NewManager(boardsDir, "", "ssh", nil)
	require.NoError(t, err)

	bus := events.NewBus()
	lockMgr := lock.NewManager(store, 30*time.Minute)

	svc := service.NewCardService(store, gitMgr, lockMgr, bus, boardsDir, nil, true, false)

	imgStore, err := images.Open(filepath.Join(tmpDir, "images.db"))
	require.NoError(t, err)

	server := NewServer(ServerConfig{
		Service:    svc,
		ImageStore: imgStore,
	})

	ctx, cancel := context.WithCancel(context.Background())

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	_, err = server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = session.Close()
		_ = imgStore.Close()

		cancel()
	})

	return &mcpImageEnv{session: session, svc: svc, store: imgStore, cancel: cancel}
}

func makeTinyPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))

	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	return buf.Bytes()
}

// createImageCard seeds a card whose body references two stored image IDs.
func createImageCard(t *testing.T, env *mcpImageEnv) (string, []string) {
	t.Helper()

	ctx := context.Background()

	id1, _, err := env.store.Put(ctx, makeTinyPNG(t))
	require.NoError(t, err)

	// Second image: slightly different bytes to get a distinct hash.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	id2, _, err := env.store.Put(ctx, buf.Bytes())
	require.NoError(t, err)

	body := fmt.Sprintf(
		"## Screenshot one\n\n![one](/api/images/%s)\n\n## Screenshot two\n\n![two](/api/images/%s)\n",
		id1, id2,
	)

	card, err := env.svc.CreateCard(ctx, "test-project", service.CreateCardInput{
		Title:    "Card with images",
		Type:     "task",
		Priority: "medium",
		Body:     body,
	})
	require.NoError(t, err)

	return card.ID, []string{id1, id2}
}

func TestGetCard_AttachesImageContent(t *testing.T) {
	env := setupMCPImages(t)

	cardID, ids := createImageCard(t, env)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_card",
		Arguments: map[string]any{"project": "test-project", "card_id": cardID},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Expect: 1 TextContent (card JSON) + 2 ImageContents.
	require.Len(t, result.Content, 3)

	_, isText := result.Content[0].(*mcp.TextContent)
	assert.True(t, isText, "first block should be card JSON")

	imgCount := 0

	gotMIME := map[string]bool{}

	for _, c := range result.Content[1:] {
		ic, ok := c.(*mcp.ImageContent)
		if !ok {
			continue
		}

		imgCount++

		assert.NotEmpty(t, ic.Data)
		gotMIME[ic.MIMEType] = true
	}

	assert.Equal(t, 2, imgCount)
	assert.True(t, gotMIME["image/png"], "expected image/png content type")
	assert.Len(t, ids, 2)
}

func TestGetCard_IncludeImagesFalse(t *testing.T) {
	env := setupMCPImages(t)

	cardID, _ := createImageCard(t, env)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project":        "test-project",
			"card_id":        cardID,
			"include_images": false,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	// Expect text-only result; the SDK auto-marshals card output to a single TextContent.
	require.Len(t, result.Content, 1)

	_, isText := result.Content[0].(*mcp.TextContent)
	assert.True(t, isText)
}

func TestGetCard_UnknownImageIDsSkipped(t *testing.T) {
	env := setupMCPImages(t)

	// Card body references one stored image and one bogus ID.
	id1, _, err := env.store.Put(context.Background(), makeTinyPNG(t))
	require.NoError(t, err)

	body := fmt.Sprintf(
		"![ok](/api/images/%s)\n![missing](/api/images/deadbeefcafebabe)\n", id1,
	)

	card, err := env.svc.CreateCard(context.Background(), "test-project", service.CreateCardInput{
		Title:    "Mixed refs",
		Type:     "task",
		Priority: "medium",
		Body:     body,
	})
	require.NoError(t, err)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_card",
		Arguments: map[string]any{"project": "test-project", "card_id": card.ID},
	})
	require.NoError(t, err)

	// Text block + 1 ImageContent (the bogus ID is dropped silently).
	require.Len(t, result.Content, 2)

	_, isImg := result.Content[1].(*mcp.ImageContent)
	assert.True(t, isImg)
}

// TestAttachImagesPinsSDKShape guards the manual json.Marshal path in
// attachImagesToResult against drift from the SDK's auto-marshal path. The
// TextContent JSON for the same card must be JSON-equivalent (semantics,
// not bytes — the SDK auto-path re-marshals via map[string]any with
// alphabetical keys) regardless of whether images are attached. Any
// divergence in field names or values means the structured-output contract
// silently differs for image-bearing cards.
func TestAttachImagesPinsSDKShape(t *testing.T) {
	env := setupMCPImages(t)

	cardID, _ := createImageCard(t, env)
	ctx := context.Background()

	withImages, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_card",
		Arguments: map[string]any{"project": "test-project", "card_id": cardID},
	})
	require.NoError(t, err)
	require.NotEmpty(t, withImages.Content)

	withoutImages, err := env.session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_card",
		Arguments: map[string]any{
			"project":        "test-project",
			"card_id":        cardID,
			"include_images": false,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, withoutImages.Content)

	withText, ok := withImages.Content[0].(*mcp.TextContent)
	require.True(t, ok, "first block in image-bearing response should be TextContent")

	withoutText, ok := withoutImages.Content[0].(*mcp.TextContent)
	require.True(t, ok, "auto-marshalled SDK response should be TextContent")

	require.JSONEq(t, withoutText.Text, withText.Text,
		"manual json.Marshal in attachImagesToResult must match SDK auto-marshal")
}

// TestLoadImageContent_ByteCapTruncates exercises the cumulative byte cap by
// passing a byteCap argument just large enough for the first image and not
// the second. The encoded PNG sizes vary slightly with the image content, so
// the cap is computed from real stored bytes — the test is robust against
// future encoder tweaks.
func TestLoadImageContent_ByteCapTruncates(t *testing.T) {
	env := setupMCPImages(t)

	ctx := context.Background()

	id1, _, err := env.store.Put(ctx, makeTinyPNG(t))
	require.NoError(t, err)

	// Second image with distinct bytes so it gets a different content hash.
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 7, G: 11, B: 13, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	id2, _, err := env.store.Put(ctx, buf.Bytes())
	require.NoError(t, err)

	// Measure the actual stored size of the first image so we can size the
	// cap exactly: first fits (total + size1 <= cap), second does not
	// (size1 + size2 > cap).
	data1, _, err := env.store.Get(ctx, id1)
	require.NoError(t, err)

	attach := attachContext{Tool: "get_card", CardID: "TEST-1"}

	// Capture slog output so we can assert that the truncation log counts the
	// image that broke the cap (not just the strictly-after positions).
	var logBuf bytes.Buffer

	origLogger := slog.Default()

	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, nil)))
	t.Cleanup(func() { slog.SetDefault(origLogger) })

	got := loadImageContent(ctx, env.store, attach, []string{id1, id2}, len(data1))
	require.Len(t, got, 1, "the byte cap must truncate the second image")

	logOut := logBuf.String()
	assert.Contains(t, logOut, "mcp: image attachment truncated by byte cap")
	assert.Contains(t, logOut, "dropped_by_cap=1",
		"the cap fires on the second of two images, so dropped_by_cap must be 1, not 0")
}

// TestLoadImageContent_ByteCapFits is the symmetric case: with the default
// cap, two tiny images both make it through. Guards against a regression
// flipping the comparator or breaking the accumulator.
func TestLoadImageContent_ByteCapFits(t *testing.T) {
	env := setupMCPImages(t)

	ctx := context.Background()

	id1, _, err := env.store.Put(ctx, makeTinyPNG(t))
	require.NoError(t, err)

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{R: 7, G: 11, B: 13, A: 255})

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))

	id2, _, err := env.store.Put(ctx, buf.Bytes())
	require.NoError(t, err)

	got := loadImageContent(ctx, env.store, attachContext{Tool: "get_card", CardID: "TEST-1"}, []string{id1, id2}, 0)
	require.Len(t, got, 2)
}

func TestGetTaskContext_AttachesImageContent(t *testing.T) {
	env := setupMCPImages(t)

	cardID, _ := createImageCard(t, env)

	result, err := env.session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_task_context",
		Arguments: map[string]any{"project": "test-project", "card_id": cardID},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	require.Len(t, result.Content, 3)

	// Verify the text block still contains the parsable task-context JSON.
	tc, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var parsed struct {
		Card *board.Card `json:"card"`
	}
	require.NoError(t, json.Unmarshal([]byte(tc.Text), &parsed))
	assert.Equal(t, cardID, parsed.Card.ID)
}
