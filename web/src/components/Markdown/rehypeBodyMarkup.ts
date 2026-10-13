import type { Element, ElementContent, Nodes, Root, RootContent } from 'hast';

// CM and agent machine markers. Hidden only at block level, where the agent
// strips them before a model reads the body; anywhere else they show as text.
const machineMarker = /^<!--\s*cm:(?:meta(?:\s+[a-z_]+=[a-z0-9_]+)*|tier=[a-z]+)\s*-->$/;

// Classes only the renderer may emit (heading anchors, copy button, icons).
const rendererClasses = new Set(['copied', 'anchor', 'octicon-link', 'octicon-copy', 'octicon-check']);

type Content = RootContent | ElementContent;

/**
 * Runs right after rehype-raw, while every element still comes from the body
 * author. Comments become visible text (block-level machine markers excepted),
 * template and non-icon svg contents are lifted out where they show, nested
 * sub/sup are flattened, renderer-owned classes and data-code are removed, and
 * each pre is reduced to its visible text so the copy payload equals what shows.
 */
export default function rehypeBodyMarkup() {
  return (tree: Root) => {
    tree.children = clean(tree.children, true, false) as RootContent[];
  };
}

function clean(nodes: Content[], atRoot: boolean, inScript: boolean): Content[] {
  return nodes.flatMap((node): Content[] => {
    if (node.type === 'comment') {
      const text = `<!--${node.value}-->`;
      return atRoot && machineMarker.test(text) ? [] : [{ type: 'text', value: text }];
    }
    if (node.type !== 'element') return [node];
    if (node.tagName === 'template') {
      const inner = clean((node.content?.children ?? []) as Content[], false, inScript);
      return [{ type: 'text', value: inner.map(textOf).join('') }];
    }

    delete node.properties.dataCode;
    stripRendererClasses(node);
    const isScript = node.tagName === 'sub' || node.tagName === 'sup';
    node.children = clean(node.children, false, inScript || isScript) as ElementContent[];
    if (isScript && inScript) return node.children;
    if (node.tagName === 'svg' && !isIcon(node)) return node.children;
    if (node.tagName === 'pre') node.children = flattenPre(node);
    return [node];
  });
}

function flattenPre(pre: Element): ElementContent[] {
  return pre.children.map((child): ElementContent => {
    const text: ElementContent = { type: 'text', value: textOf(child) };
    return child.type === 'element' && child.tagName === 'code' ? { ...child, children: [text] } : text;
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

function textOf(node: Nodes | undefined): string {
  if (!node) return '';
  if (node.type === 'text') return node.value;
  return 'children' in node ? node.children.map(textOf).join('') : '';
}
