import { describe, it, expect } from 'vitest';
import {
  acceptedLabel,
  assumptionChip,
  basisRefs,
  crossFindings,
  httpUrl,
  parseAcceptance,
  parseAssumptions,
  parseBoard,
  parseCards,
  parseGrounding,
  parseSignoff,
  parseSharedDecisions,
  parseSlices,
  refTone,
  stageAssumptions,
  stageOutcomes,
  withoutTags,
} from './artifactData';
import { ACCEPTANCE_V1, BOARD_V1, CARDS_V1, GROUNDING_V1, SHARED_V1, SHARED_V2, SIGNOFF_V1 } from '../../test-utils/blueprintFixtures';

describe('artifact data', () => {
  it("reads a criterion's and a manual check's outcomes and the slice's edge-case sweep", () => {
    const [slice] = parseAcceptance({
      slices: [
        {
          id: 'SL1',
          criteria: [{ id: 'AC1', project: 'p', text: 't', outcomes: ['O1', 2] }],
          manual: [{ id: 'MC1', text: 'm' }],
          sweep: [{ condition: 'Limits', settled_by: ['AC1'] }, { condition: 'Permissions', not_applicable: 'No auth.' }, { settled_by: ['AC1'] }],
        },
      ],
    })!;
    expect(slice.criteria[0].outcomes).toEqual(['O1']);
    expect(slice.manual[0].outcomes).toEqual([]);
    expect(slice.sweep).toEqual([
      { condition: 'Limits', settledBy: ['AC1'], notApplicable: '' },
      { condition: 'Permissions', settledBy: [], notApplicable: 'No auth.' },
    ]);
    expect(parseAcceptance(ACCEPTANCE_V1.data)![0].sweep).toEqual([]);
  });

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

  it('reads a board card type and priority, empty on an older board', () => {
    const data = BOARD_V1.data as { cards: object[] };
    const typed = parseBoard({ ...data, cards: [{ ...data.cards[0], type: 'bug', priority: 'high' }, ...data.cards.slice(1)] })!;
    expect(typed.cards[0]).toMatchObject({ type: 'bug', priority: 'high' });
    for (const c of parseBoard(BOARD_V1.data)!.cards) expect(c).toMatchObject({ type: '', priority: '' });
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

describe('findings across the blueprint', () => {
  it('reads them critical first, then major, minor and any other severity, and skips junk', () => {
    const cross = [
      { id: 'X1', severity: 'minor', text: 'A3 and A7 say the same thing.', refs: ['A3', 'A7', 4] },
      { id: 'X2', severity: 'constructor', text: 'Odd.' },
      { id: 'X3', severity: 'critical', text: 'DEC2 keeps field order but D3 sorts keys.', refs: ['DEC2', 'D3'] },
      { id: 'X4', severity: 'major', text: 'No criterion proves O2.', refs: ['O2'] },
      { id: 'X5', severity: 'major' },
    ];
    const found = crossFindings({ cross });
    expect(found.map((f) => f.id)).toEqual(['X3', 'X4', 'X1', 'X2']);
    expect(found[2].refs).toEqual(['A3', 'A7']);
    expect(crossFindings({ cards: [] })).toEqual([]);
    expect(crossFindings('nope')).toEqual([]);
  });

  it("reads the sign-off's findings and override reason, none on an older sign-off", () => {
    const data = { ...(SIGNOFF_V1.data as object), cross: [{ id: 'X1', severity: 'critical', text: 'T.' }], override_reason: 'False positive.' };
    expect(parseSignoff(data)).toMatchObject({ overrideReason: 'False positive.', cross: [{ id: 'X1', severity: 'critical', text: 'T.', refs: [] }] });
    expect(parseSignoff(SIGNOFF_V1.data)).toMatchObject({ overrideReason: '', cross: [] });
  });
});

describe('cards left not ready', () => {
  it('reads not_ready as given, empty when absent, dropping non-strings', () => {
    const entries = ['D3: Finding.', 'D6: not ready'];
    expect(parseSignoff({ ...(SIGNOFF_V1.data as object), not_ready: entries })!.notReady).toEqual(entries);
    expect(parseSignoff(SIGNOFF_V1.data)!.notReady).toEqual([]);
    expect(parseSignoff({ ...(SIGNOFF_V1.data as object), not_ready: ['D3: F.', 7, null] })!.notReady).toEqual(['D3: F.']);
  });
});

describe('a plain-language sign-off', () => {
  it('reads consequences, acceptances, superseded answers and the summary, skipping junk', () => {
    const data = parseSignoff({
      ...(SIGNOFF_V1.data as object),
      assumptions: [
        { id: 'A1', stage: 'intake', text: 'Items sync through a hosted database.', status: 'assumed', consequence: 'Adding items needs internet.', consequence_accepted: { by: 'human', round: 2 } },
        { id: 'Q4', stage: 'intake', text: 'Offline edits are dropped.', status: 'assumed', superseded_by: 'Q7' },
        { id: 'A2', stage: 'intake', text: 'T.', status: 'assumed', consequence_accepted: 'yes' },
        { id: 'A3', stage: 'intake', text: 'U.', status: 'assumed', consequence_accepted: { by: 'human', round: 'two' } },
        { id: 'A4', stage: 'intake', text: 'V.', status: 'assumed', consequence_accepted: { by: 'human', round: 0 } },
        { id: 'A5', stage: 'intake', text: 'W.', status: 'assumed', consequence_accepted: { by: 'human', round: 1.5 } },
      ],
      plain_summary: {
        build: ['Shopping list: one list both phones share', 7],
        can_do: ['Add, tick off and remove items'],
        accepted: [{ id: 'A1', text: 'Adding items needs internet.', changed: true }, { id: 'A9' }, 'junk'],
      },
    })!;
    expect(data.assumptions.map((a) => [a.id, a.status, a.consequence, a.accepted, a.supersededBy])).toEqual([
      ['A1', 'assumed', 'Adding items needs internet.', { by: 'human', round: 2 }, ''],
      ['Q4', 'assumed', '', null, 'Q7'],
      ['A2', 'assumed', '', null, ''],
      ['A3', 'assumed', '', null, ''],
      ['A4', 'assumed', '', null, ''],
      ['A5', 'assumed', '', null, ''],
    ]);
    expect(data.plainSummary).toEqual({
      build: ['Shopping list: one list both phones share'],
      canDo: ['Add, tick off and remove items'],
      accepted: [{ id: 'A1', text: 'Adding items needs internet.', changed: true }],
    });
  });

  it('has no summary on a sign-off without one, or with nothing in it', () => {
    expect(parseSignoff(SIGNOFF_V1.data)!.plainSummary).toBeNull();
    expect(parseSignoff({ ...(SIGNOFF_V1.data as object), plain_summary: { build: [], can_do: [], accepted: [] } })!.plainSummary).toBeNull();
    expect(parseSignoff({ ...(SIGNOFF_V1.data as object), plain_summary: 'x' })!.plainSummary).toBeNull();
  });

  it('names the person who accepted a consequence as the sheet names your answers', () => {
    expect(acceptedLabel({ by: 'human', round: 2 })).toBe(acceptedLabel({ by: '', round: 2 }));
    expect(acceptedLabel({ by: 'human', round: 2 })).not.toContain('human');
    expect(acceptedLabel({ by: 'ops-team', round: 2 })).toContain('ops-team');
    expect(acceptedLabel({ by: 'human', round: 2 })).not.toBe(acceptedLabel({ by: 'human', round: 3 }));
  });
});

describe('notices', () => {
  it('reads notices on the sign-off and the board, empty when absent or null', () => {
    expect(parseSignoff({ ...(SIGNOFF_V1.data as object), notices: ['N.'] })!.notices).toEqual(['N.']);
    expect(parseBoard(BOARD_V1.data)!.notices).toEqual([]);
    const nulled = parseSignoff({ ...(SIGNOFF_V1.data as object), notices: null, steps: null })!;
    expect(nulled.notices).toEqual([]);
    expect(nulled.steps).toEqual([]);
  });
});
