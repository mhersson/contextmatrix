export const MIN_VIEWPORT_WIDTH = 320;
export const MAX_VIEWPORT_WIDTH = 2560;

const META_START = /<meta(?=[\s/>])/gi;
// One attribute, or the closing `>`, per match: a broken tag is read once, never rescanned.
const ATTRIBUTE = /[\s/]*(?:(>)|([^\s/>][^\s/>=]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>"'][^\s>]*)))?)/y;
const WIDTH_ENTRY = /^\s*width\s*=\s*(\d+)\s*$/i;

/** Attributes of the tag whose first one starts at `from`, and where the tag ends; null if it never closes. */
function readTag(source: string, from: number): { attrs: Map<string, string>; end: number } | null {
  const attrs = new Map<string, string>();
  ATTRIBUTE.lastIndex = from;
  for (;;) {
    const m = ATTRIBUTE.exec(source);
    if (!m) return null;
    if (m[1] !== undefined) return { attrs, end: ATTRIBUTE.lastIndex };
    const name = m[2].toLowerCase();
    if (!attrs.has(name)) attrs.set(name, m[3] ?? m[4] ?? m[5] ?? '');
  }
}

function widthOf(content: string): number | null {
  for (const entry of content.split(/[,;]/)) {
    const m = WIDTH_ENTRY.exec(entry);
    if (m) return Math.min(MAX_VIEWPORT_WIDTH, Math.max(MIN_VIEWPORT_WIDTH, Number(m[1])));
  }
  return null;
}

/** The CSS px width a mockup source declares with `<meta name="viewport" content="width=N">`, clamped;
 *  null when it declares none. Scans tags as text: model-written HTML is never parsed into a DOM here. */
export function viewportWidth(source: string): number | null {
  META_START.lastIndex = 0;
  for (let start = META_START.exec(source); start; start = META_START.exec(source)) {
    const tag = readTag(source, start.index + '<meta'.length);
    if (!tag) return null;
    if (tag.attrs.get('name')?.trim().toLowerCase() === 'viewport') return widthOf(tag.attrs.get('content') ?? '');
    META_START.lastIndex = tag.end;
  }
  return null;
}
