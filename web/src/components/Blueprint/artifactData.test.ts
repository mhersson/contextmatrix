import { describe, it, expect } from 'vitest';
import { assumptionChip, basisRefs, httpUrl, parseAssumptions, parseBoard, parseCards, parseGrounding, withoutTags } from './artifactData';
import { BOARD_V1, CARDS_V1, GROUNDING_V1 } from '../../test-utils/blueprintFixtures';

describe('artifact data', () => {
  it('reads assumptions as sentences or objects and skips junk', () => {
    expect(parseAssumptions(['A.', { text: 'B.', basis: 'Q1' }, 7, { basis: 'Q2' }, ''])).toEqual([
      { id: '', text: 'A.', basis: '', status: '', evidence: '' },
      { id: '', text: 'B.', basis: 'Q1', status: '', evidence: '' },
    ]);
    expect(parseAssumptions(null)).toEqual([]);
  });

  it('labels each assumption status and claims none it does not have', () => {
    const a = { id: 'A1', text: 't', basis: 'Q3', status: '', evidence: '[S2] x' };
    expect(assumptionChip({ ...a, status: 'assumed' })).toEqual({ tone: 'purple', label: 'assumed by responder (Q3)' });
    expect(assumptionChip({ ...a, status: 'confirmed' })).toEqual({ tone: 'blue', label: 'confirmed by you (Q3)' });
    expect(assumptionChip({ ...a, status: 'verified' })).toEqual({ tone: 'green', label: 'verified S2' });
    expect(assumptionChip({ ...a, status: '' })).toEqual({ tone: 'grey', label: 'Q3' });
    expect(assumptionChip({ ...a, status: '', basis: '' })).toBeNull();
  });

  it('splits a basis into Q and S references', () => {
    expect(basisRefs('Q1, S2 and the assumption')).toEqual({ refs: ['Q1', 'S2'], rest: 'and the assumption' });
    expect(withoutTags('[S1] Encode ends with a newline.')).toBe('Encode ends with a newline.');
  });

  it('reads grounding projects sorted by name', () => {
    expect(parseGrounding(GROUNDING_V1.data)!.projects.map((p) => p.name)).toEqual(['emptyrepo', 'homelander']);
  });

  it('counts places and criteria per card', () => {
    const d1 = parseCards(CARDS_V1.data)!.cards[0];
    expect(d1).toMatchObject({ id: 'D1', places: 2, criteria: 2, dependsOn: [] });
  });

  it('reads the board and links only http(s) URLs', () => {
    const board = parseBoard(BOARD_V1.data)!;
    expect(board.cards.map((c) => c.card)).toEqual(['HOMELAND-012', 'HOMELAND-013', 'HOMELAND-014']);
    expect(board.cards[2]).toMatchObject({ draft: 'D3', state: '', pr: '' });
    expect(board.manual).toEqual(['curl prints one key per line.']);
    expect(board.priced).toBe(true);
    expect(httpUrl(board.cards[0].pr)).toBe('https://github.com/example/homelander/pull/7');
    expect(httpUrl(board.cards[1].pr)).toBeNull();
  });

  it('returns null for data that is not an object', () => {
    expect(parseCards('nope')).toBeNull();
    expect(parseBoard(undefined)).toBeNull();
  });
});
