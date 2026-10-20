import { useState, type ComponentPropsWithoutRef } from 'react';
import type { Element, ElementContent } from 'hast';
import { MockupFrame } from './MockupFrame';
import { MOCKUP_LABELS } from './labels';

export function MockupBlock({ source }: { source: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="mk-block">
      <button type="button" className="bf-btn-ghost bf-btn-sm" aria-expanded={open} onClick={() => setOpen(!open)}>
        {open ? MOCKUP_LABELS.hide : MOCKUP_LABELS.show}
      </button>
      {open && <MockupFrame source={source} title={MOCKUP_LABELS.frame} />}
    </div>
  );
}

function textOf(node: ElementContent): string {
  if (node.type === 'text') return node.value;
  return node.type === 'element' ? node.children.map(textOf).join('') : '';
}

function mockupCode(node: Element | undefined): Element | undefined {
  return node?.children.find(
    (child): child is Element =>
      child.type === 'element' &&
      child.tagName === 'code' &&
      [child.properties.className].flat().includes('language-mockup'),
  );
}

type PreProps = ComponentPropsWithoutRef<'pre'> & { node?: Element };

/** react-markdown `pre` renderer: a mockup fence becomes the sealed block, any other stays a `pre`. */
export function MockupAwarePre({ node, ...props }: PreProps) {
  const code = mockupCode(node);
  if (code) return <MockupBlock source={code.children.map(textOf).join('')} />;
  return <pre {...props} />;
}
