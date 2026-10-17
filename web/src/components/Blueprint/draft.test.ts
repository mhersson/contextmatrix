import { describe, it, expect } from 'vitest';
import { applyDraft, codePoints, holdDraft } from './draft';

const frame = (offset: number, text: string, extra = {}) => ({ stage: 'solution_design', version: 2, offset, text, ...extra });

describe('drafts', () => {
  it('replaces the held text at offset 0', () => {
    const held = holdDraft(frame(0, 'Old'));
    expect(applyDraft(held, frame(0, 'New'))).toMatchObject({ text: 'New', length: 3 });
  });

  it('appends at the held length counted in code points', () => {
    const held = holdDraft(frame(0, 'Hi \u{1F44D}'));
    expect(held.length).toBe(4);
    expect(codePoints('Hi \u{1F44D}')).toBe(4);
    expect(applyDraft(held, frame(4, ' ok'))).toMatchObject({ text: 'Hi \u{1F44D} ok', length: 7 });
  });

  it('drops a frame at any other offset and keeps the held draft', () => {
    const held = holdDraft(frame(0, 'Hi \u{1F44D}'));
    expect(applyDraft(held, frame(5, ' ok'))).toBe(held);
    expect(applyDraft(held, { ...frame(4, ' ok'), version: 3 })).toBe(held);
    expect(applyDraft(null, frame(4, ' ok'))).toBeNull();
  });

  it('marks a refused attempt until the next attempt restarts at 0', () => {
    const held = holdDraft(frame(0, 'Draft'));
    const refused = applyDraft(held, frame(5, '', { refused: true, reason: 'no decisions' }))!;
    expect(refused).toMatchObject({ text: 'Draft', refused: true, reason: 'no decisions' });
    expect(applyDraft(refused, frame(0, 'Again'))).toMatchObject({ text: 'Again', refused: false });
  });
});
