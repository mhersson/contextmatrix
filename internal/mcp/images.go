package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mhersson/contextmatrix/internal/images"
)

// maxAttachedImages caps how many image blocks a single tool response can
// carry. Screenshot-heavy cards otherwise risk blowing the agent's context
// window. The cap is intentionally low — agents needing more can fetch images
// individually via `GET /api/images/{id}`.
const maxAttachedImages = 10

// mdImage matches a markdown image reference: `![alt](url)`. The URL portion
// is captured for downstream filtering. Square brackets in alt text are
// allowed except for the literal `]` that closes the alt.
var mdImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)]+)\)`)

// cmImageURL matches both relative (`/api/images/<hex>`) and absolute
// (`https://host/api/images/<hex>`) URLs hosted by this server. The id capture
// group enforces the 16-hex-char ID format produced by images.Store.
var cmImageURL = regexp.MustCompile(`^(?:https?://[^/]+)?/api/images/([a-f0-9]{16})$`)

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

// attachImagesToResult builds a CallToolResult that pairs the JSON-marshaled
// `output` (rendered as a single TextContent so the existing SDK contract for
// structured output stays intact) with inline ImageContent blocks for every
// cm-server image referenced in `body`.
//
// When `include` is non-nil and false, no images are attached and the function
// returns nil — the SDK's default auto-marshal of `output` then takes over.
// When no referenced images resolve, also returns nil for the same reason.
func attachImagesToResult(ctx context.Context, store images.Store, output any, body string, include *bool) *mcp.CallToolResult {
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

	imgs := loadImageContent(ctx, store, ids)
	if len(imgs) == 0 {
		return nil
	}

	// Marshal output to a TextContent block so the SDK's structured-output
	// merge still surfaces the JSON to legacy clients alongside the image
	// blocks.
	payload, err := json.Marshal(output)
	if err != nil {
		slog.Warn("mcp: marshal output for image attachment failed", "error", err)

		return nil
	}

	content := make([]mcp.Content, 0, 1+len(imgs))
	content = append(content, &mcp.TextContent{Text: string(payload)})
	content = append(content, imgs...)

	return &mcp.CallToolResult{Content: content}
}

// loadImageContent fetches each image from store and returns MCP ImageContent
// blocks in input order. Unknown IDs (ErrNotFound) are silently skipped so a
// dangling reference in a card body does not break the whole tool call.
// Transport errors are logged and the affected image is skipped.
func loadImageContent(ctx context.Context, store images.Store, ids []string) []mcp.Content {
	if store == nil || len(ids) == 0 {
		return nil
	}

	out := make([]mcp.Content, 0, len(ids))

	for _, id := range ids {
		data, contentType, err := store.Get(ctx, id)
		if err != nil {
			if !errors.Is(err, images.ErrNotFound) {
				slog.Warn("mcp: image fetch failed", "id", id, "error", err)
			}

			continue
		}

		out = append(out, &mcp.ImageContent{
			Data:     data,
			MIMEType: contentType,
		})
	}

	return out
}
