/**
 * safeUrlTransform - allowlist-based URL filter for MarkdownPreview.
 *
 * @uiw/react-markdown-preview@5.x sets defaultUrlTransform = url => url,
 * overriding react-markdown's built-in scheme allowlist. This restores a safe
 * default: only well-known navigable schemes are passed through; everything
 * else (e.g. javascript:, data:, vbscript:) is replaced with an empty string
 * so the browser renders a harmless no-op link.
 *
 * Pass as the `urlTransform` prop on every <MarkdownPreview> usage.
 */
const ALLOWED_SCHEMES = ['https:', 'http:', 'mailto:', 'xmpp:'];

export function safeUrlTransform(url: string): string {
  // Allow relative URLs: fragments, query-string-only, and path-relative refs.
  if (url.startsWith('#') || url.startsWith('?') || url.startsWith('/')) {
    return url;
  }

  try {
    const parsed = new URL(url);
    if (ALLOWED_SCHEMES.includes(parsed.protocol)) {
      return url;
    }
  } catch {
    // Not an absolute URL - treat as relative and allow it through.
    return url;
  }

  // Reject anything with an unsupported scheme (javascript:, data:, etc.).
  return '';
}
