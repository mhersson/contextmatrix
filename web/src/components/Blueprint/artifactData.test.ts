import { describe, it, expect } from 'vitest';
import {
  assumptionChip,
  basisRefs,
  httpUrl,
  parseAssumptions,
  parseBoard,
  parseCards,
  parseGrounding,
  parseSlices,
  refTone,
  stageAssumptions,
  withoutTags,
} from './artifactData';
import { BOARD_V1, CARDS_V1, GROUNDING_V1 } from '../../test-utils/blueprintFixtures';

describe('artifact data', () => {
  it('reads assumptions as sentences or objects and skips junk', () => {
    const none = { id: '', basis: '', status: '', evidence: '', answer: '', stage: '' };
    expect(parseAssumptions(['A.', { text: 'B.', basis: 'Q1' }, 7, { basis: 'Q2' }, ''])).toEqual([
      { ...none, text: 'A.' },
      { ...none, text: 'B.', basis: 'Q1' },
    ]);
    expect(parseAssumptions(null)).toEqual([]);
  });

  it('keeps the answer an assumption rests on and the stage that stated it', () => {
    const [a] = parseAssumptions([{ id: 'A2', text: 'T.', basis: 'Q14', answer: 'Configurable', stage: 'intake', status: 'confirmed' }]);
    expect(a).toMatchObject({ answer: 'Configurable', stage: 'intake' });
  });

  it('reads the assumptions a stage stated beside its other data', () => {
    expect(stageAssumptions({ slices: [], assumptions: [{ id: 'A11', text: 'T.', status: 'unconfirmed' }] })).toMatchObject([
      { id: 'A11', text: 'T.', status: 'unconfirmed' },
    ]);
    expect(stageAssumptions(['T.'])).toEqual([]);
  });

  it('labels each assumption status apart from its basis and claims none it does not have', () => {
    const a = { id: 'A1', text: 't', basis: 'Q3', status: '', evidence: '[S2] x', answer: '', stage: '' };
    expect(assumptionChip({ ...a, status: 'assumed' })).toEqual({ tone: 'purple', label: 'assumed by responder' });
    expect(assumptionChip({ ...a, status: 'confirmed' })).toEqual({ tone: 'blue', label: 'confirmed by you' });
    expect(assumptionChip({ ...a, status: 'verified' })).toEqual({ tone: 'green', label: 'verified' });
    expect(assumptionChip({ ...a, status: 'unverified' })).toEqual({ tone: 'yellow', label: 'unverified' });
    expect(assumptionChip({ ...a, status: 'contradicted' })).toEqual({ tone: 'red', label: 'contradicted' });
    expect(assumptionChip({ ...a, status: 'unconfirmed, awaiting user confirmation' })).toEqual({ tone: 'yellow', label: 'unconfirmed' });
    expect(assumptionChip({ ...a, status: 'pending review' })).toEqual({ tone: 'grey', label: 'pending review' });
    expect(assumptionChip({ ...a, status: '' })).toBeNull();
  });

  it('splits a basis into question, round answer and source references', () => {
    expect(basisRefs('Q1, S2 and the assumption')).toEqual({ refs: ['Q1', 'S2'], rest: 'and the assumption' });
    expect(basisRefs('round2.A1; S3')).toEqual({ refs: ['round2.A1', 'S3'], rest: '' });
    expect(withoutTags('[S1] Encode ends with a newline.')).toBe('Encode ends with a newline.');
  });

  it('tones answers apart from sources', () => {
    expect(['Q1', 'round2.A1', 'S3'].map(refTone)).toEqual(['blue', 'blue', 'green']);
  });

  it('keeps the slices each slice is built after', () => {
    const slices = parseSlices({ slices: [{ id: 'SL1', title: 'T', depends_on: ['SL2', 3] }, { id: 'SL2', title: 'U' }] })!;
    expect(slices.map((s) => s.dependsOn)).toEqual([['SL2'], []]);
  });

  it('keeps the criteria a manual check covers', () => {
    expect(parseCards(CARDS_V1.data)!.manual).toEqual([{ id: 'M1', text: 'Run curl against the server.', criteria: ['MC1'] }]);
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
