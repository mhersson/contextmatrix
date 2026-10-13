import type { Options } from 'rehype-sanitize';

// Prism classes @uiw's markdown.css colors, plus the line and anchor-icon
// markers. A free class list would let a body borrow the app's utility
// classes (fixed, inset-0, hidden) to overlay or hide content.
const codeSpanClasses = [
  'token', 'code-line', 'line-number', 'highlight-line', 'icon', 'icon-link',
  'atrule', 'attr-name', 'attr-value', 'bold', 'boolean', 'builtin', 'cdata', 'char', 'class',
  'class-name', 'color', 'comment', 'constant', 'coord', 'deleted', 'doctype', 'entity', 'function',
  'important', 'inserted', 'italic', 'keyword', 'maybe-class-name', 'namespace', 'number', 'operator',
  'prolog', 'property', 'property-access', 'punctuation', 'regex', 'rule', 'selector', 'string',
  'symbol', 'tag', 'url', 'variable',
];

const alignments = ['align', 'left', 'center', 'right'] as const;

/**
 * Everything a card body may render: GFM output, the renderer's own heading
 * anchors, alerts, code highlighting and copy button, and the safe HTML GitHub
 * issues carry. svg and path appear only with icon geometry. No style, no
 * event handlers, no frames or forms; ids are prefixed so a body cannot
 * clobber document properties.
 */
export const markdownSchema: Options = {
  tagNames: [
    'a', 'b', 'blockquote', 'br', 'code', 'del', 'details', 'div', 'em', 'h1', 'h2', 'h3', 'h4',
    'h5', 'h6', 'hr', 'i', 'img', 'input', 'kbd', 'li', 'ol', 'p', 'path', 'pre', 'section', 'span',
    'strong', 'sub', 'summary', 'sup', 'svg', 'table', 'tbody', 'td', 'th', 'thead', 'tr', 'ul',
  ],
  attributes: {
    a: [
      'href', 'title', 'name', 'id', 'ariaLabel', 'ariaDescribedBy', ['ariaHidden', 'true'], ['tabIndex', -1],
      'dataFootnoteRef', 'dataFootnoteBackref', ['className', 'anchor', 'data-footnote-backref'],
    ],
    code: [['className', /^language-./, 'code-highlight']],
    details: ['open'],
    div: [
      ['className', 'markdown-alert', /^markdown-alert-(?:note|tip|important|warning|caution)$/, 'copied'],
      ['dir', 'auto'],
      'dataCode',
    ],
    h1: ['id'], h2: ['id'], h3: ['id'], h4: ['id'], h5: ['id'], h6: ['id'],
    img: ['src', 'alt', 'title', 'width', 'height'],
    input: [['type', 'checkbox'], ['disabled', true], 'checked'],
    li: ['id', ['className', 'task-list-item']],
    ol: ['start', ['className', 'contains-task-list']],
    p: [['className', 'markdown-alert-title'], ['dir', 'auto']],
    path: ['d', ['fillRule', 'evenodd']],
    pre: [['className', /^language-./]],
    section: ['dataFootnotes', ['className', 'footnotes']],
    span: ['line', ['className', ...codeSpanClasses]],
    svg: [
      'viewBox', 'width', 'height', ['ariaHidden', 'true'], ['fill', 'currentColor'], ['version', '1.1'],
      ['className', 'octicon', 'octicon-link', 'octicon-copy', 'octicon-check'],
    ],
    td: [[...alignments]],
    th: [[...alignments]],
    ul: [['className', 'contains-task-list']],
  },
  ancestors: { tbody: ['table'], td: ['table'], th: ['table'], thead: ['table'], tr: ['table'], path: ['svg'] },
  clobber: ['ariaDescribedBy', 'ariaLabelledBy', 'id', 'name'],
  clobberPrefix: 'user-content-',
  protocols: { href: ['http', 'https', 'mailto', 'xmpp'], src: ['http', 'https'] },
  required: { input: { type: 'checkbox', disabled: true } },
  // Unwrap, never drop: a script or style body shows as text instead of
  // vanishing from the human reader while an agent still reads it.
  strip: [],
  allowComments: false,
  allowDoctypes: false,
};
