import { describe, it, expect } from 'vitest';
import { parseStructured, utf8Length, formatBytes } from './chatEntryUtils';

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

describe('utf8Length', () => {
  it('counts UTF-8 bytes', () => {
    expect(utf8Length('a€😀')).toBe(1 + 3 + 4);
  });
});

describe('formatBytes', () => {
  it('formats bytes', () => {
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(65536)).toBe('64 KiB');
    expect(formatBytes(65537)).toBe('64.1 KiB');
    expect(formatBytes(12697)).toBe('12.4 KiB');
  });
});
