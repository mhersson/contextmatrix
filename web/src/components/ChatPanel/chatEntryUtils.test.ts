import { describe, it, expect } from 'vitest';
import { parseStructured } from './chatEntryUtils';

describe('parseStructured', () => {
  it('pretty-prints a JSON object', () => {
    expect(parseStructured('{"approved":false,"fixes":[1,2]}')).toBe(
      JSON.stringify({ approved: false, fixes: [1, 2] }, null, 2),
    );
  });

  it('pretty-prints a JSON array', () => {
    expect(parseStructured('[{"a":1}]')).toBe(JSON.stringify([{ a: 1 }], null, 2));
  });

  it('tolerates surrounding whitespace', () => {
    expect(parseStructured('  {"ok":true}\n')).toBe(JSON.stringify({ ok: true }, null, 2));
  });

  it('returns null for prose, markdown links, scalars, and truncated JSON', () => {
    expect(parseStructured('Just a normal message')).toBeNull();
    expect(parseStructured('[see the docs](https://example.com)')).toBeNull();
    expect(parseStructured('42')).toBeNull();
    expect(parseStructured('true')).toBeNull();
    expect(parseStructured('"quoted string"')).toBeNull();
    expect(parseStructured('{"cut":"mid')).toBeNull();
  });
});
