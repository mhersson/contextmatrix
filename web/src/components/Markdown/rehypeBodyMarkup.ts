import type { Comment, Element, ElementContent, Nodes, Root, RootContent } from 'hast';
import { markdownSchema } from './markdownSchema';

// CM and agent machine markers. Hidden only where the agent strips them: on the
// raw source, at the start of a line after nothing but spaces or tabs.
const machineMarker = /^<!--[ \t]*cm:(?:meta(?:[ \t]+[a-z_]+=[a-z0-9_]+)*|tier=[a-z]+)[ \t]*-->$/;

// Classes only the renderer may emit (heading anchors, copy button, icons).
const rendererClasses = new Set(['copied', 'anchor', 'octicon-link', 'octicon-copy', 'octicon-check']);

const renderedTags = new Set(markdownSchema.tagNames ?? []);
const requiredAncestors = markdownSchema.ancestors ?? {};

// Nested members of a group compound their shrinking or fading; only the
// outermost renders.
const nestingGroups: Record<string, string> = {
  sub: 'script', sup: 'script', code: 'code',
  h1: 'heading', h2: 'heading', h3: 'heading', h4: 'heading', h5: 'heading', h6: 'heading',
};

// rehype-raw's positions index this value, which lacks container prefixes
// (list indent, `> `), so they match the source only up to `exact`.
interface RawChunk {
  start: number;
  value: string;
  exact: number;
}

interface SourceFile {
  value: unknown;
}

interface Context {
  source: string;
  chunks: readonly RawChunk[];
  groups: ReadonlySet<string>;
  ancestors: readonly string[];
}

type Content = RootContent | ElementContent;
type RawTree = { type: string; value?: unknown; position?: Root['position']; children?: RawTree[] };

const rawChunks = new WeakMap<SourceFile, RawChunk[]>();

/** Runs right before rehype-raw and records the raw HTML chunks it will parse. */
export function rehypeRawChunks() {
  return (tree: Root, file: SourceFile) => {
    const chunks: RawChunk[] = [];
    rawChunks.set(file, collectRaw(tree as RawTree, String(file.value), chunks) ? chunks : []);
  };
}

/**
 * Runs right after rehype-raw, while every element is still author- or
 * markdown-made: shows what the allowlist will not render as literal text, and
 * strips what lets author markup hide text or pose as the renderer's own.
 */
export default function rehypeBodyMarkup() {
  return (tree: Root, file: SourceFile) => {
    const source = String(file.value);
    const chunks = rawChunks.get(file) ?? [];
    tree.children = clean(tree.children, { source, chunks, groups: new Set(), ancestors: [] }) as RootContent[];
  };
}

function collectRaw(node: RawTree, source: string, chunks: RawChunk[]): boolean {
  if (node.type === 'raw') {
    const start = node.position?.start.offset;
    // rehype-raw would carry on the previous chunk's offsets, so none are safe.
    if (start === undefined) return false;
    const value = String(node.value);
    let exact = 0;
    while (exact < value.length && value[exact] === source[start + exact]) exact++;
    chunks.push({ start, value, exact });
  }
  return (node.children ?? []).every((child) => collectRaw(child, source, chunks));
}

function clean(nodes: Content[], context: Context): Content[] {
  return nodes.flatMap((node): Content[] => {
    if (node.type === 'comment') {
      return isMachineMarker(node, context) ? [] : [text(`<!--${node.value}-->`)];
    }
    if (node.type !== 'element') return [node];
    if (node.tagName === 'template') {
      const inner = clean((node.content?.children ?? []) as Content[], context);
      return [text(inner.map(textOf).join(''))];
    }
    if (!isRendered(node, context.ancestors)) {
      return escapeTags(node, clean(node.children, context) as ElementContent[], context.chunks);
    }

    const group = nestingGroups[node.tagName];
    const children = clean(node.children, {
      ...context,
      groups: group ? new Set([...context.groups, group]) : context.groups,
      ancestors: [...context.ancestors, node.tagName],
    }) as ElementContent[];
    if (group && context.groups.has(group)) return children;

    node.children = children;
    delete node.properties.dataCode;
    delete node.properties.tabIndex;
    // @uiw turns a root heading's aria-hidden first child into its permalink,
    // replacing that child's content; an icon svg has no text to lose.
    if (node.tagName !== 'svg') delete node.properties.ariaHidden;
    // Every span class the schema allows is added by later renderer steps.
    if (node.tagName === 'span') delete node.properties.className;
    else stripRendererClasses(node);
    if (node.tagName === 'pre') node.children = flattenPre(node);
    return [node];
  });
}

function isMachineMarker(comment: Comment, { source, chunks }: Context): boolean {
  const start = comment.position?.start.offset;
  const end = comment.position?.end.offset;
  const chunk = chunkAt(chunks, start, false);
  if (!chunk || start === undefined || end === undefined || end - chunk.start > chunk.exact) return false;
  const lineStart = source.lastIndexOf('\n', start - 1) + 1;
  return /^[ \t]*$/.test(source.slice(lineStart, start)) && machineMarker.test(source.slice(start, end));
}

function isRendered(node: Element, ancestors: readonly string[]): boolean {
  const required = requiredAncestors[node.tagName];
  return (
    renderedTags.has(node.tagName) &&
    (node.tagName !== 'svg' || isIcon(node)) &&
    (!required || required.some((tag) => ancestors.includes(tag)))
  );
}

// Shows the element's own tags, as parsed, around its content. Without a
// readable start tag it unwraps; an implied end tag is never invented.
function escapeTags(node: Element, children: ElementContent[], chunks: readonly RawChunk[]): ElementContent[] {
  const start = node.position?.start.offset;
  const opening = chunkAt(chunks, start, false);
  if (!opening || start === undefined) return children;
  const from = start - opening.start;
  const to = startTagEnd(opening.value, from);
  const startTag = opening.value.slice(from, to);
  if (to < 0 || !opensTag(startTag, node.tagName)) return children;

  const end = node.position?.end.offset;
  const closing = chunkAt(chunks, end, true);
  if (!closing || end === undefined) return [text(startTag), ...children];
  const endTag = endTagBefore(closing.value.slice(0, end - closing.start), node.tagName);
  // An end tag inside the content closed a nested element of the same name.
  const own = endTag !== undefined && end - endTag.length >= Math.max(opening.start + to, contentEnd(node.children));
  return [text(startTag), ...children, ...(own ? [text(endTag)] : [])];
}

function chunkAt(chunks: readonly RawChunk[], offset: number | undefined, isEnd: boolean): RawChunk | undefined {
  if (offset === undefined) return undefined;
  return chunks.find((chunk) => {
    const index = offset - chunk.start;
    return isEnd ? index > 0 && index <= chunk.value.length : index >= 0 && index < chunk.value.length;
  });
}

function opensTag(tag: string, name: string): boolean {
  return tag.slice(0, name.length + 1).toLowerCase() === `<${name.toLowerCase()}` && /[\t\n\f\r />]/.test(tag[name.length + 1] ?? '');
}

// The tokenizer's rule: a quote opens a value only right after `=`.
function startTagEnd(html: string, start: number): number {
  let state = 'name';
  for (let i = start + 1; i < html.length; i++) {
    const c = html[i];
    const space = /[\t\n\f\r ]/.test(c);
    if (state === '"' || state === "'") {
      if (c === state) state = 'before';
    } else if (c === '>') {
      return i + 1;
    } else if (state === 'name' || state === 'unquoted') {
      if (space || (state === 'name' && c === '/')) state = 'before';
    } else if (state === 'before') {
      if (!space && c !== '/') state = 'attribute';
    } else if (state === 'attribute' || state === 'after') {
      if (c === '=') state = 'value';
      else if (c === '/') state = 'before';
      else state = space ? 'after' : 'attribute';
    } else if (state === 'value') {
      if (c === '"' || c === "'") state = c;
      else if (!space) state = 'unquoted';
    }
  }
  return -1;
}

function endTagBefore(html: string, name: string): string | undefined {
  const match = /^<\/([^\t\n\f\r />]+)[ \t\n]*>$/.exec(html.slice(html.lastIndexOf('</')));
  return match && match[1].toLowerCase() === name.toLowerCase() ? match[0] : undefined;
}

function contentEnd(nodes: readonly ElementContent[]): number {
  return nodes.reduce(
    (max, child) => Math.max(max, child.position?.end?.offset ?? ('children' in child ? contentEnd(child.children) : -1)),
    -1,
  );
}

function flattenPre(pre: Element): ElementContent[] {
  return pre.children.map((child): ElementContent => {
    const value = text(textOf(child));
    return child.type === 'element' && child.tagName === 'code' ? { ...child, children: [value] } : value;
  });
}

function stripRendererClasses(node: Element) {
  const value = node.properties.className;
  if (value === undefined) return;
  const kept = (Array.isArray(value) ? value : String(value).split(/\s+/)).filter(
    (name) => !rendererClasses.has(String(name)),
  );
  if (kept.length > 0) node.properties.className = kept as string[];
  else delete node.properties.className;
}

function isIcon(svg: Element): boolean {
  return svg.children.every((child) => child.type === 'element' && child.tagName === 'path' && child.children.length === 0);
}

function text(value: string): ElementContent {
  return { type: 'text', value };
}

function textOf(node: Nodes | undefined): string {
  if (!node) return '';
  if (node.type === 'text') return node.value;
  return 'children' in node ? node.children.map(textOf).join('') : '';
}
