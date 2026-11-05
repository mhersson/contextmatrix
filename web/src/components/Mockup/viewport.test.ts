import { describe, it, expect } from 'vitest';
import { viewportWidth, MIN_VIEWPORT_WIDTH, MAX_VIEWPORT_WIDTH } from './viewport';

const page = (head: string) => `<!doctype html><html><head>${head}</head><body><p>hi</p></body></html>`;

describe('viewportWidth', () => {
  it('clamps to the bounds it exports', () => {
    expect([MIN_VIEWPORT_WIDTH, MAX_VIEWPORT_WIDTH]).toEqual([320, 2560]);
  });

  it.each<[string, string, number | null]>([
    ['a bare width', '<meta name="viewport" content="width=1280">', 1280],
    ['a width among other keys', '<meta name="viewport" content="width=1280, initial-scale=1">', 1280],
    ['a width after another key', '<meta name="viewport" content="initial-scale=1, width=1280">', 1280],
    ['a semicolon separator', '<meta name="viewport" content="initial-scale=1; width=1280">', 1280],
    ['attributes reversed', '<meta content="width=1280" name="viewport">', 1280],
    ['single quotes', "<meta name='viewport' content='width=1280'>", 1280],
    ['no quotes', '<meta name=viewport content=width=1280>', 1280],
    ['a self-closed tag', '<meta name="viewport" content="width=1280" />', 1280],
    ['an upper-case name value', '<meta NAME="Viewport" content="width=1280">', 1280],
    ['an upper-case width key', '<meta name="viewport" content="WIDTH=1280">', 1280],
    ['spaces around the equals sign', '<meta name="viewport" content="width = 390">', 390],
    ['a padded name value', '<meta name=" viewport " content="width=1280">', 1280],
    ['a tag split over lines', '<meta\n  name="viewport"\n  content="width=1280"\n>', 1280],
    ['a meta charset before it', '<meta charset="utf-8"><meta name="viewport" content="width=1280">', 1280],
    ['another named meta before it', '<meta name="description" content="width=999"><meta name="viewport" content="width=1280">', 1280],
    [
      'a quoted greater-than sign in an earlier tag',
      '<meta name="description" content="a > b"><meta name="viewport" content="width=1280">',
      1280,
    ],
    [
      'two viewport tags, the first wins',
      '<meta name="viewport" content="width=1280"><meta name="viewport" content="width=800">',
      1280,
    ],
    [
      'two viewport tags, the first without a width wins',
      '<meta name="viewport" content="width=device-width"><meta name="viewport" content="width=1280">',
      null,
    ],
    ['device-width', '<meta name="viewport" content="width=device-width, initial-scale=1">', null],
    ['a fractional width', '<meta name="viewport" content="width=12.5">', null],
    ['a non-numeric width', '<meta name="viewport" content="width=abc">', null],
    ['a width with a unit', '<meta name="viewport" content="width=1280px">', null],
    ['an empty width', '<meta name="viewport" content="width=">', null],
    ['no width key', '<meta name="viewport" content="initial-scale=1">', null],
    ['no content', '<meta name="viewport">', null],
    ['a key that only ends in width', '<meta name="viewport" content="min-width=1280">', null],
    ['a width on a meta that is not the viewport', '<meta name="theme" content="width=1280">', null],
    ['a width in the name attribute', '<meta name="width=1280" content="viewport">', null],
    ['below the minimum', '<meta name="viewport" content="width=100">', 320],
    ['zero', '<meta name="viewport" content="width=0">', 320],
    ['above the maximum', '<meta name="viewport" content="width=9999">', 2560],
    ['more digits than a number holds', `<meta name="viewport" content="width=${'9'.repeat(400)}">`, 2560],
  ])('reads %s', (_label, head, want) => {
    expect(viewportWidth(page(head))).toBe(want);
  });

  it.each<[string, string]>([
    ['no meta at all', '<!doctype html><p>hello</p>'],
    ['an empty source', ''],
    ['a source with a meta-looking word', '<p>a <metal> thing</p>'],
    ['an unterminated tag', '<meta name="viewport" content="width=1280"'],
    ['an unterminated quote', '<meta name="viewport content="width=1280>'],
  ])('finds nothing in %s', (_label, source) => {
    expect(viewportWidth(source)).toBeNull();
  });

  it('reads a tag that is not inside a head', () => {
    expect(viewportWidth('<meta name="viewport" content="width=1280"><p>bare</p>')).toBe(1280);
  });

  it('finishes quickly on a long source with many unclosed tags', () => {
    const source = '<meta name="a '.repeat(20000);
    const started = performance.now();
    expect(viewportWidth(source)).toBeNull();
    expect(performance.now() - started).toBeLessThan(1000);
  });
});
