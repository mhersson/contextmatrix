import type { Element, ElementContent, Nodes, Root, RootContent } from 'hast';

// CM and agent machine markers. Strict on purpose: a comment that only looks
// like one stays visible, so a marker cannot carry hidden text.
const machineMarker = /^<!--\s*cm:(?:meta(?:\s+[a-z_]+=[a-z0-9_]+)*|tier=[a-z]+)\s*-->$/;

/**
 * Runs right after rehype-raw, while every element still comes from the body
 * author. Comments become visible text (machine markers excepted), template
 * and non-icon svg contents are lifted out where they show, and data-code is
 * removed so only the renderer's copy button can choose clipboard text.
 */
export default function rehypeBodyMarkup() {
  return (tree: Root) => {
    tree.children = clean(tree.children) as RootContent[];
  };
}

function clean(nodes: Array<RootContent | ElementContent>): Array<RootContent | ElementContent> {
  return nodes.flatMap((node): Array<RootContent | ElementContent> => {
    if (node.type === 'comment') {
      const text = `<!--${node.value}-->`;
      return machineMarker.test(text) ? [] : [{ type: 'text', value: text }];
    }
    if (node.type !== 'element') return [node];
    if (node.tagName === 'template') return [{ type: 'text', value: textOf(node.content) }];

    delete node.properties.dataCode;
    node.children = clean(node.children) as ElementContent[];
    return node.tagName === 'svg' && !isIcon(node) ? node.children : [node];
  });
}

function isIcon(svg: Element): boolean {
  return svg.children.every((child) => child.type === 'element' && child.tagName === 'path' && child.children.length === 0);
}

function textOf(node: Nodes | undefined): string {
  if (!node) return '';
  if (node.type === 'text') return node.value;
  return 'children' in node ? node.children.map(textOf).join('') : '';
}
