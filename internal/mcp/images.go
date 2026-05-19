package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"github.com/mhersson/contextmatrix/internal/images"
)

// maxAttachedImages caps how many image blocks a single tool response can
// carry. Screenshot-heavy cards otherwise risk blowing the agent's context
// window. The cap is intentionally low — agents needing more can fetch images
// individually via `GET /api/images/{id}`.
const maxAttachedImages = 10

// defaultMaxAttachedImageBytes caps the *cumulative* image-byte budget for a
// single tool response. A 1024x768 PNG can easily reach 1–3 MiB and the SDK
// base64-encodes inline image content, so ten max-sized images would otherwise
// push past 30 MiB on the wire. Truncating at ~20 MiB worth of raw bytes keeps
// the response shape predictable while still leaving headroom for several
// screenshots. Truncation is logged so an operator can correlate.
//
// Tests override the cap by passing it explicitly to loadImageContent rather
// than mutating package state; production callers pass 0 and let the helper
// resolve to this default internally.
const defaultMaxAttachedImageBytes = 20 << 20

// imageFetchConcurrency caps the number of in-flight store.Get calls when
// loading inline image content. The image store backs onto SQLite with
// MaxOpenConns(5); a cap of 4 leaves a connection free for other traffic
// while still cutting wall time for screenshot-heavy cards.
const imageFetchConcurrency = 4

// mdImage matches a markdown image reference: `![alt](url)`. The URL portion
// is captured for downstream filtering. Square brackets in alt text are
// allowed except for the literal `]` that closes the alt.
var mdImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// cmImageURL matches both relative (`/api/images/<hex>`) and absolute
// (`https://host/api/images/<hex>`) URLs hosted by this server. The id capture
// group enforces the canonical ID shape produced by images.Store; the
// pattern fragment is sourced from the images package so any future change
// to the ID alphabet/length propagates here automatically.
var cmImageURL = regexp.MustCompile(`^(?:https?://[^/]+)?/api/images/(` + images.IDPatternFragment + `)$`)

// extractCMImageIDs returns up to maxAttachedImages unique cm-server image
// IDs referenced in body, in order of first appearance.
func extractCMImageIDs(body string) []string {
	matches := mdImage.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))

	for _, m := range matches {
		if len(m) < 2 {
			continue
		}

		sub := cmImageURL.FindStringSubmatch(m[1])
		if len(sub) < 2 {
			continue
		}

		id := sub[1]
		if _, ok := seen[id]; ok {
			continue
		}

		seen[id] = struct{}{}

		ids = append(ids, id)

		if len(ids) >= maxAttachedImages {
			break
		}
	}

	if len(ids) == 0 {
		return nil
	}

	return ids
}

// attachContext bundles the metadata that attachImagesToResult and
// loadImageContent emit on truncation / fetch-failure logs. `Tool` is the
// MCP tool name (e.g. "get_card"); `CardID` is the primary card whose body
// produced the image refs. Both fields are server-side constants — they
// never carry user input — so logging them verbatim is safe. Unexported
// because no caller outside this package needs to construct one.
type attachContext struct {
	Tool   string
	CardID string
}

// attachImagesToResult builds a CallToolResult that pairs the JSON-marshaled
// `output` (rendered as a single TextContent so the existing SDK contract for
// structured output stays intact) with inline ImageContent blocks for every
// cm-server image referenced in `body`.
//
// When `include` is non-nil and false, no images are attached and the function
// returns nil — the SDK's default auto-marshal of `output` then takes over.
// When no referenced images resolve, also returns nil for the same reason.
//
// Production callers pass byteCap == 0 to use defaultMaxAttachedImageBytes;
// the parameter exists so tests can exercise the cumulative-byte-cap
// truncation branch without staging tens of megabytes of synthetic PNG bytes.
//
// The TextContent JSON shape must remain semantically equivalent to what the
// SDK would auto-marshal for the same output (see TestAttachImagesPinsSDKShape,
// which uses require.JSONEq). The SDK auto-path re-marshals through
// map[string]any (alphabetical keys), so bytes may differ even when JSON
// semantics match — drift in field names or values would still silently
// change the structured data an agent sees only on image-bearing cards.
func attachImagesToResult(ctx context.Context, store images.Store, attach attachContext, output any, body string, include *bool, byteCap int) *mcp.CallToolResult {
	if include != nil && !*include {
		return nil
	}

	if store == nil {
		return nil
	}

	ids := extractCMImageIDs(body)
	if len(ids) == 0 {
		return nil
	}

	imgs := loadImageContent(ctx, store, attach, ids, byteCap)
	if len(imgs) == 0 {
		return nil
	}

	// Marshal output to a TextContent block so the SDK's structured-output
	// merge still surfaces the JSON to legacy clients alongside the image
	// blocks.
	payload, err := json.Marshal(output)
	if err != nil {
		slog.Warn("mcp: marshal output for image attachment failed",
			"tool", attach.Tool, "card_id", attach.CardID,
			"image_count", len(imgs), "error", err)

		return nil
	}

	content := make([]mcp.Content, 0, 1+len(imgs))
	content = append(content, &mcp.TextContent{Text: string(payload)})

	for _, img := range imgs {
		content = append(content, img)
	}

	return &mcp.CallToolResult{Content: content}
}

// loadImageContent fetches each image from store and returns ImageContent
// blocks in input order. Unknown IDs (ErrNotFound) are silently skipped so a
// dangling reference in a card body does not break the whole tool call.
// Transport errors are logged and the affected image is skipped.
//
// Per-ID fetches fan out through an errgroup with a small concurrency cap;
// results are reassembled in input order so the cumulative byte cap applies
// deterministically against the same body order an agent sees. Appending an
// image that would exceed the budget stops the assembly, and the truncation
// is logged so the operator can correlate. Truncating at the first
// budget-breaking image (rather than skipping and continuing) keeps the
// response order predictable for the agent.
//
// byteCap <= 0 falls back to defaultMaxAttachedImageBytes; this is the only
// place the default is referenced. Tests pass a small cap explicitly.
func loadImageContent(ctx context.Context, store images.Store, attach attachContext, ids []string, byteCap int) []*mcp.ImageContent {
	if store == nil || len(ids) == 0 {
		return nil
	}

	if byteCap <= 0 {
		byteCap = defaultMaxAttachedImageBytes
	}

	// fetched[i] is the result of fetching ids[i]; nil entries are skipped
	// (ErrNotFound or transport error) and never count against the cap.
	fetched := make([]*mcp.ImageContent, len(ids))

	var g errgroup.Group

	g.SetLimit(imageFetchConcurrency)

	// store.Get is safe for concurrent use; the warn-log helper is the only
	// shared state we touch from worker goroutines.
	var logMu sync.Mutex

	for i, id := range ids {
		g.Go(func() error {
			data, contentType, err := store.Get(ctx, id)
			if err != nil {
				if !errors.Is(err, images.ErrNotFound) {
					logMu.Lock()
					slog.Warn("mcp: image fetch failed",
						"tool", attach.Tool, "card_id", attach.CardID,
						"id", id, "error", err)
					logMu.Unlock()
				}

				return nil
			}

			fetched[i] = &mcp.ImageContent{
				Data:     data,
				MIMEType: contentType,
			}

			return nil
		})
	}

	// Per-fetch errors are logged-and-swallowed above; the group's combined
	// error is always nil and Wait is only called to synchronize.
	_ = g.Wait()

	out := make([]*mcp.ImageContent, 0, len(ids))

	var total int

	for i, img := range fetched {
		if img == nil {
			continue
		}

		if total+len(img.Data) > byteCap {
			// `dropped_by_cap` counts images whose bytes were fetched but
			// dropped because the cumulative byte budget was exhausted — this
			// includes the current image at i (which broke the cap) plus any
			// non-nil entries after it. ErrNotFound entries at later positions
			// are nil here and logged separately during fetch above, so they
			// are excluded from this count.
			droppedByCap := 0

			for k := i; k < len(fetched); k++ {
				if fetched[k] != nil {
					droppedByCap++
				}
			}

			slog.Warn("mcp: image attachment truncated by byte cap",
				"tool", attach.Tool,
				"card_id", attach.CardID,
				"cap_bytes", byteCap,
				"attached", len(out),
				"dropped_by_cap", droppedByCap,
			)

			break
		}

		total += len(img.Data)
		out = append(out, img)
	}

	return out
}
