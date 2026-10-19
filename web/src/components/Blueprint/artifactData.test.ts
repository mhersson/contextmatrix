import { describe, it, expect } from 'vitest';
import {
  assumptionChip,
  basisRefs,
  httpUrl,
  parseAssumptions,
  parseBoard,
  parseCards,
  parseGrounding,
  parseSharedDecisions,
  parseSlices,
  refTone,
  stageAssumptions,
  stageOutcomes,
  withoutTags,
} from './artifactData';
import { BOARD_V1, CARDS_V1, GROUNDING_V1, SHARED_V1, SHARED_V2 } from '../../test-utils/blueprintFixtures';

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

  it('chips each assumption status in its tone, apart from its basis and sources, and claims none it does not have', () => {
    const a = { id: 'A1', text: 't', basis: 'Q3', status: '', evidence: '[S2] x', answer: '', stage: '' };
    const chip = (status: string) => assumptionChip({ ...a, status });
    const statuses = ['unverified', 'unconfirmed', 'contradicted', 'assumed', 'confirmed', 'verified', 'pending review'];
    expect(statuses.map((s) => chip(s)!.tone)).toEqual(['yellow', 'yellow', 'red', 'purple', 'blue', 'green', 'grey']);
    for (const s of statuses) expect(chip(s)!.label).not.toMatch(/Q3|S2/);
    expect(chip('unconfirmed, awaiting user confirmation')).toEqual(chip('unconfirmed'));
    expect(chip('pending review')!.label).toBe('pending review');
    expect(chip('')).toBeNull();
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

describe('parseSharedDecisions', () => {
  it('reads one decision list with contracts and assumptions', () => {
    const data = parseSharedDecisions(SHARED_V1.data)!;
    expect(data.decisions.map((d) => d.id)).toEqual(['DEC1', 'DEC2']);
    expect(data.decisions[0].rejected).toEqual([{ option: 'Helm chart', why: 'templates are hard to review' }]);
    expect(data.contracts).toEqual(['Every service logs JSON to stdout.']);
    expect(data.assumptions.map((a) => a.basis)).toEqual(['', 'Q2']);
  });

  it('treats missing contracts as none and junk as no data', () => {
    expect(parseSharedDecisions(SHARED_V2.data)?.contracts).toEqual([]);
    expect(parseSharedDecisions('nope')).toBeNull();
  });
});

describe('outcomes and basis references', () => {
  it("reads a stage's outcomes with their quality and skips junk", () => {
    const data = { outcomes: [{ id: 'O1', text: 'Under 50 ms at p95.', quality: 'performance' }, { id: 'O2', text: 'Indented JSON.' }, { id: 'O3' }, 'junk'] };
    expect(stageOutcomes(data)).toEqual([
      { id: 'O1', text: 'Under 50 ms at p95.', quality: 'performance' },
      { id: 'O2', text: 'Indented JSON.', quality: '' },
    ]);
    expect(stageOutcomes({ assumptions: [] })).toEqual([]);
    expect(stageOutcomes('nope')).toEqual([]);
  });

  it('chips stated assumptions and outcomes beside questions, round answers and sources', () => {
    expect(basisRefs('A3, O2 and Q1')).toEqual({ refs: ['A3', 'O2', 'Q1'], rest: 'and' });
    expect(basisRefs('round2.A1 restates A1')).toEqual({ refs: ['round2.A1', 'A1'], rest: 'restates' });
    expect(basisRefs('AC4, SL2 and DEC2')).toEqual({ refs: [], rest: 'AC4 SL2 and DEC2' });
  });

  it('tones assumptions and outcomes apart from answers and sources', () => {
    expect(['A3', 'O2', 'Q1', 'round2.A1', 'S3'].map(refTone)).toEqual(['grey', 'grey', 'blue', 'blue', 'green']);
  });
});
