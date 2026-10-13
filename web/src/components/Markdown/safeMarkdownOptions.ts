import rehypeAttrs from 'rehype-attr';
import rehypeIgnore from 'rehype-ignore';
import rehypeRaw from 'rehype-raw';
import rehypeSanitize from 'rehype-sanitize';
import { find, html } from 'property-information';
import type { Element, Root } from 'hast';
import type { MarkdownPreviewProps } from '@uiw/react-markdown-preview';
import { safeUrlTransform } from '../../utils/safeUrlTransform';
import { markdownSchema } from './markdownSchema';
import rehypeBodyMarkup from './rehypeBodyMarkup';

type PluginList = Parameters<NonNullable<MarkdownPreviewProps['pluginsFilter']>>[1];

// rehype-attr copies `<!--rehype:k=v-->` comments onto elements as attributes;
// rehype-ignore hides text from the human reader that an agent still receives.
const droppedPlugins = new Set<unknown>([rehypeAttrs, rehypeIgnore]);

const clobberPrefix = 'user-content-';

// Elements the final step emitted; allowElement rejects everything else.
const sanitizedElements = new WeakSet<Element>();

/** @uiw's list with one guarded rehype-raw and the sanitizer forced last. */
export function secureRehypePlugins(plugins: PluginList): PluginList {
  const result: PluginList = [];
  let rawSeen = false;

  for (const plugin of plugins) {
    const attacher = Array.isArray(plugin) ? plugin[0] : plugin;
    if (droppedPlugins.has(attacher)) continue;
    if (attacher === rehypeRaw) {
      if (rawSeen) continue;
      rawSeen = true;
      result.push(plugin, rehypeBodyMarkup);
      continue;
    }
    result.push(plugin);
  }

  result.push(rehypeSanitizeLast);
  return result;
}

/** Final rehype step: sanitize against the allowlist and record what survives. */
export function rehypeSanitizeLast() {
  const sanitize = rehypeSanitize(markdownSchema);

  return (tree: Root): Root => {
    prepare(tree);
    const safe = sanitize(tree);
    record(safe);
    return safe;
  };
}

// Steps after rehype-raw write attribute spellings (`class`, `data-code`) the
// sanitizer's property-name schema would strip; in-page links gain the prefix
// the sanitizer gives every id; leftover raw HTML stays visible as text.
function prepare(parent: Root | Element) {
  parent.children = parent.children.map((node) => {
    if ((node as { type: string }).type === 'raw') {
      return { type: 'text', value: (node as unknown as { value: string }).value };
    }
    if (node.type === 'element') {
      normalizeProperties(node);
      const href = node.properties.href;
      if (typeof href === 'string' && href.length > 1 && href.startsWith('#') && !href.startsWith(`#${clobberPrefix}`)) {
        node.properties.href = `#${clobberPrefix}${href.slice(1)}`;
      }
      prepare(node);
    }
    return node;
  }) as typeof parent.children;
}

function normalizeProperties(node: Element) {
  for (const [key, value] of Object.entries(node.properties)) {
    const info = find(html, key);
    const normalized = info.spaceSeparated && typeof value === 'string' ? value.split(/\s+/).filter(Boolean) : value;
    if (info.property !== key) delete node.properties[key];
    const existing = node.properties[info.property];
    node.properties[info.property] =
      info.property !== key && Array.isArray(existing) && Array.isArray(normalized)
        ? [...existing, ...normalized]
        : normalized;
  }
}

function record(node: Root | Root['children'][number]) {
  if (node.type === 'element') sanitizedElements.add(node);
  if ('children' in node) node.children.forEach(record);
}

export const safeMarkdownOptions = {
  // @uiw inverts this flag: true stops it appending a second rehype-raw.
  skipHtml: true,
  urlTransform: safeUrlTransform,
  pluginsFilter: (type, plugins) => (type === 'rehype' ? secureRehypePlugins(plugins) : plugins),
  // The sanitizer prefixes ids; footnotes must not arrive prefixed already.
  remarkRehypeOptions: { clobberPrefix: '' },
  allowElement: (element) => sanitizedElements.has(element),
  unwrapDisallowed: true,
} satisfies MarkdownPreviewProps;
