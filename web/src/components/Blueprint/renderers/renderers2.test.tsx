import { describe, it, expect, vi } from 'vitest';
import { render, screen, within, fireEvent } from '@testing-library/react';
import { ArtifactRenderer } from './ArtifactRenderer';
import { BOARD_V1, CARDS_V1, READINESS_V2, SIGNOFF_V1, ref, viewAtDesignGate } from '../../../test-utils/blueprintFixtures';
import type { BlueprintArtifact } from '../../../types';
import { acceptedLabel, assumptionChip, CHANGED_SINCE_ACCEPTED, supersededLabel } from '../artifactData';
import { stageText } from '../stages';

vi.mock('../../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source, mockups }: { source: string; mockups?: boolean }) => (
    <div data-testid="md" data-mockups={mockups ? 'on' : undefined}>
      {source}
    </div>
  ),
}));

const view = viewAtDesignGate();
const show = (artifact: BlueprintArtifact) =>
  render(<ArtifactRenderer artifact={artifact} previous={null} mode="plain" view={view} />);
const sectionOf = (heading: RegExp) => screen.getByRole('heading', { name: heading }).closest('section')!;
const cellsIn = (section: HTMLElement, text: string) =>
  within(within(section).getByText(text).closest('tr')!)
    .getAllByRole('cell')
    .map((c) => c.textContent);

const CROSS = [
  { id: 'X1', severity: 'minor', text: 'A3 and A7 say the same thing.', refs: ['A3', 'A7'] },
  { id: 'X2', severity: 'critical', text: 'DEC2 keeps field order but D3 sorts keys.', refs: ['DEC2', 'D3'] },
  { id: 'X3', severity: 'major', text: 'No criterion proves O2.', refs: ['O2'] },
];
const withCross = (a: BlueprintArtifact, over: object = {}): BlueprintArtifact => ({
  ...a,
  data: { ...(a.data as object), cross: CROSS, ...over },
});
const firstCells = (section: HTMLElement) =>
  within(section)
    .getAllByRole('row')
    .slice(1)
    .map((r) => within(r).getAllByRole('cell')[0].textContent);

describe('CardsSheet', () => {
  it('previews each card with its project, type, priority, places, criteria and dependencies', () => {
    show(CARDS_V1);
    const d1 = screen.getByRole('article', { name: 'D1 Pretty-print the GET / response' });
    expect(within(d1).getByText('homelander')).toBeInTheDocument();
    expect(within(d1).getByText('feature')).toBeInTheDocument();
    expect(within(d1).getByText('medium')).toBeInTheDocument();
    expect(within(d1).getByText('2 places')).toBeInTheDocument();
    expect(within(d1).getByText('2 criteria')).toBeInTheDocument();
    expect(within(d1).getByText('no dependencies')).toBeInTheDocument();
    expect(within(d1).getByText('Card body')).toBeInTheDocument();
    const d3 = screen.getByRole('article', { name: 'D3 Document both endpoints' });
    expect(within(d3).getByText('after D1, D2')).toBeInTheDocument();
  });

  it("renders the mockup blocks in each card's body", () => {
    show(CARDS_V1);
    const d1 = screen.getByRole('article', { name: 'D1 Pretty-print the GET / response' });
    fireEvent.click(within(d1).getByText('Card body'));
    expect(within(d1).getByTestId('md')).toHaveAttribute('data-mockups', 'on');
  });

  it('adds the playbook order for several cards', () => {
    show(CARDS_V1);
    const order = screen.getByRole('heading', { name: /Playbook order/ }).closest('section')!;
    expect(within(order).getAllByRole('listitem').map((li) => li.querySelector('.bp-id')?.textContent)).toEqual([
      'D1',
      'D2',
      'D3',
      'M1',
    ]);
  });

  it('labels a manual check in the playbook order and no card', () => {
    show(CARDS_V1);
    const order = screen.getByRole('heading', { name: /Playbook order/ }).closest('section')!;
    const item = (id: string) => within(order).getByText(id, { selector: '.bp-id' }).closest('li')!;
    expect(item('M1')).toHaveTextContent('manual check');
    expect(item('D1')).not.toHaveTextContent('manual check');
  });

  it('shows each manual check once when the playbook order already lists it', () => {
    show(CARDS_V1);
    expect(screen.getAllByText('M1', { selector: '.bp-id' })).toHaveLength(1);
    expect(screen.queryByRole('heading', { name: /^Manual checks/ })).not.toBeInTheDocument();
  });

  it('keeps a manual check missing from the order under Manual checks without repeating the ordered ones', () => {
    const data = CARDS_V1.data as { manual: { id: string; text: string; criteria: string[] }[] };
    show({
      ...CARDS_V1,
      data: { ...data, manual: [...data.manual, { id: 'M2', text: 'Open the health page.', criteria: ['MC2', 'MC3'] }] },
    });
    const checks = sectionOf(/^Manual checks/);
    expect(cellsIn(checks, 'Open the health page.')).toEqual(['M2', 'Open the health page.', 'MC2, MC3']);
    expect(within(checks).queryByText('M1')).not.toBeInTheDocument();
    expect(screen.getAllByText('M1', { selector: '.bp-id' })).toHaveLength(1);
  });

  it('gives each manual check of a single card a row with the checks it covers', () => {
    const data = CARDS_V1.data as { cards: unknown[]; manual: { id: string; text: string }[] };
    show({ ...CARDS_V1, data: { ...data, cards: data.cards.slice(0, 1), order: ['D1', 'M1'] } });
    expect(screen.queryByRole('heading', { name: /Playbook order/ })).not.toBeInTheDocument();
    expect(cellsIn(sectionOf(/^Manual checks/), 'Run curl against the server.')).toEqual(['M1', 'Run curl against the server.', 'MC1']);
  });
});

describe('ReadinessSheet', () => {
  function inLoop() {
    const v = viewAtDesignGate();
    v.pipeline = v.pipeline.map((s) => (s.stage === 'readiness' ? { ...s, status: 'running' as const, round: 2, max_rounds: 3 } : s));
    v.artifacts = [...v.artifacts, ref('readiness', 1, 'superseded'), ref('readiness', 2, 'current')];
    return v;
  }

  it('lists the findings across the blueprint above the cards, critical first', () => {
    const { container } = render(<ArtifactRenderer artifact={withCross(READINESS_V2)} previous={null} mode="plain" view={inLoop()} />);
    const table = sectionOf(/^Across the blueprint/);
    expect(firstCells(table)).toEqual(['X2', 'X3', 'X1']);
    expect(cellsIn(table, 'DEC2 keeps field order but D3 sorts keys.')).toEqual(['X2', 'critical', 'DEC2 keeps field order but D3 sorts keys.', 'DEC2 D3']);
    expect(within(table).getByText('critical')).toHaveClass('bp-sev--critical');
    expect(table.compareDocumentPosition(container.querySelector('.bp-readiness')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('shows an unknown severity neutrally', () => {
    render(<ArtifactRenderer artifact={withCross(READINESS_V2, { cross: [{ id: 'X9', severity: 'blocker', text: 'Odd.' }] })} previous={null} mode="plain" view={inLoop()} />);
    expect(within(sectionOf(/^Across the blueprint/)).getByText('blocker')).toHaveAttribute('class', 'bp-sev');
  });

  it('shows no findings table for a version stored before readiness judged the whole blueprint', () => {
    render(<ArtifactRenderer artifact={READINESS_V2} previous={null} mode="plain" view={inLoop()} />);
    expect(screen.queryByRole('heading', { name: /^Across the blueprint/ })).not.toBeInTheDocument();
  });

  it('shows the loop round on the newest version and each card ready or its findings', () => {
    render(<ArtifactRenderer artifact={READINESS_V2} previous={null} mode="plain" view={inLoop()} />);
    expect(screen.getByText('Round 2 of 3')).toBeInTheDocument();
    expect(screen.getByText('Ready')).toBeInTheDocument();
    expect(screen.getByText('The README heading is `### GET /`.')).toBeInTheDocument();
  });

  it('shows no round on an older version', () => {
    render(<ArtifactRenderer artifact={{ ...READINESS_V2, version: 1 }} previous={null} mode="plain" view={inLoop()} />);
    expect(screen.queryByText(/^Round/)).not.toBeInTheDocument();
  });

  it('folds the written readiness under the cards and opens it in compare mode', () => {
    const { container, rerender } = render(<ArtifactRenderer artifact={READINESS_V2} previous={null} mode="plain" view={inLoop()} />);
    const details = container.querySelector('details')!;
    expect(details.open).toBe(false);
    expect(details).toHaveTextContent(READINESS_V2.body.split('\n').find((l) => l.trim().length > 8)!.trim());
    expect(screen.getByText('Ready')).toBeInTheDocument();
    rerender(<ArtifactRenderer artifact={READINESS_V2} previous={READINESS_V2} mode="compare" view={inLoop()} />);
    expect(container.querySelector('details')!.open).toBe(true);
  });

  it('renders the body plainly when there are no cards', () => {
    const { container } = render(<ArtifactRenderer artifact={{ ...READINESS_V2, data: undefined }} previous={null} mode="plain" view={inLoop()} />);
    expect(container.querySelector('details')).toBeNull();
    expect(screen.getByText('Round 2 of 3')).toBeInTheDocument();
    expect(screen.getByTestId('md')).toHaveTextContent(READINESS_V2.body.split('\n').find((l) => l.trim().length > 8)!.trim());
  });
});

describe('SignoffSheet', () => {
  const signoffWith = (over: object) => ({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), ...over } });
  const status = (s: string) => assumptionChip({ id: '', text: '', basis: '', evidence: '', answer: '', stage: '', status: s })!.label;
  const stage = (s: string) => stageText(s).name;

  it('gives each run step a row with its place, what it does and its kind', () => {
    show(SIGNOFF_V1);
    const order = sectionOf(/^Run order/);
    expect(cellsIn(order, 'Pretty-print the GET / response')).toEqual(['1', 'D1', 'Pretty-print the GET / response', 'feature']);
    const manual = cellsIn(order, 'curl prints one key per line.');
    expect(manual.slice(0, 3)).toEqual(['2', 'M1', 'curl prints one key per line.']);
    expect(manual[3]).not.toBe('');
    expect(manual[3]).not.toBe('feature');
  });

  it('gives each assumption a row with your answer, its status, its basis and sources, and its stage', () => {
    show(SIGNOFF_V1);
    const assumptions = sectionOf(/^Assumptions/);
    expect(cellsIn(assumptions, 'Two-space indentation.')).toEqual(['A2', 'Two-space indentation.', 'Two spaces', status('confirmed'), 'Q1', stage('intake')]);
    expect(cellsIn(assumptions, 'No README example.')).toEqual(['A3', 'No README example.', '', status('assumed'), 'Q3', stage('solution_design')]);
    expect(cellsIn(assumptions, 'Clients ignore whitespace.')).toEqual(['A1', 'Clients ignore whitespace.', '', status('unverified'), '', stage('intake')]);
    expect(cellsIn(assumptions, 'The README has an example.')).toEqual(['A5', 'The README has an example.', '', status('contradicted'), 'S2', stage('grounding')]);
  });

  it('puts the evidence under a verified assumption', () => {
    show(SIGNOFF_V1);
    expect(cellsIn(sectionOf(/^Assumptions/), 'Encode ends with a newline.')).toEqual([
      'A4',
      'Encode ends with a newline.main.go',
      '',
      status('verified'),
      'S1',
      stage('grounding'),
    ]);
  });

  it('chips an answer accepted on a round as a basis', () => {
    show(signoffWith({ assumptions: [{ id: 'A1', stage: 'intake', text: 'T.', basis: 'round2.A1', status: 'confirmed' }] }));
    expect(within(sectionOf(/^Assumptions/)).getByText('round2.A1')).toBeInTheDocument();
  });

  it('shows an answer only on an assumption you confirmed, never the responder\'s', () => {
    show(
      signoffWith({
        assumptions: [
          { id: 'A1', stage: 'intake', text: 'T1.', basis: 'Q1', answer: 'Two spaces', status: 'confirmed' },
          { id: 'A2', stage: 'intake', text: 'T2.', basis: 'Q2', answer: 'Tabs', status: 'assumed' },
        ],
      }),
    );
    const assumptions = sectionOf(/^Assumptions/);
    expect(cellsIn(assumptions, 'T1.')[2]).toBe('Two spaces');
    expect(cellsIn(assumptions, 'T2.')[2]).toBe('');
  });

  it('leaves out the answer column when no assumption carries an answer', () => {
    show(signoffWith({ assumptions: [{ id: 'A1', stage: 'intake', text: 'T.', basis: 'Q1', status: 'confirmed' }] }));
    const assumptions = sectionOf(/^Assumptions/);
    expect(within(assumptions).getAllByRole('columnheader')).toHaveLength(5);
    expect(cellsIn(assumptions, 'T.')).toEqual(['A1', 'T.', status('confirmed'), 'Q1', stage('intake')]);
  });

  it('gives each manual check after the run a row and keeps the warnings and setup steps', () => {
    show(SIGNOFF_V1);
    expect(cellsIn(sectionOf(/^Manual checks after the run/), 'curl prints one key per line.')).toEqual(['M1', 'curl prints one key per line.']);
    expect(screen.getByText('One assumption is unverified.')).toBeInTheDocument();
    expect(screen.getByText(/set verify command/)).toBeInTheDocument();
  });

  it('falls back to prose assumptions from an older blueprint', () => {
    show({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), assumptions: 'A1 holds.' } });
    expect(screen.getByText('A1 holds.')).toBeInTheDocument();
  });

  it('opens with the findings across the blueprint and the reason you approved over a critical one', () => {
    show(withCross(SIGNOFF_V1, { override_reason: 'X2 is a false positive; D3 keeps the order.' }));
    expect(screen.getAllByRole('heading', { level: 3 })[0]).toHaveTextContent(/^Across the blueprint/);
    expect(firstCells(sectionOf(/^Across the blueprint/))).toEqual(['X2', 'X3', 'X1']);
    expect(screen.getByRole('note', { name: 'Override' })).toHaveTextContent('X2 is a false positive; D3 keeps the order.');
  });

  it('shows neither on a sign-off stored before them', () => {
    show(SIGNOFF_V1);
    expect(screen.queryByRole('heading', { name: /^Across the blueprint/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('note', { name: 'Override' })).not.toBeInTheDocument();
  });

  it('says when the preflight could not run', () => {
    show({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), preflight_error: 'repo unreachable' } });
    expect(screen.getByRole('note', { name: 'Preflight' })).toHaveTextContent('repo unreachable');
  });
  describe('on a plain-language blueprint', () => {
    const plain = signoffWith({
      assumptions: [
        { id: 'A1', stage: 'intake', text: 'Items sync through a hosted database.', basis: 'Q2', status: 'assumed', consequence: 'Adding items needs internet.', consequence_accepted: { by: 'human', round: 2 } },
        { id: 'Q4', stage: 'intake', text: 'Offline edits are dropped.', basis: 'Q4', status: 'assumed', superseded_by: 'Q7' },
      ],
      plain_summary: {
        build: ['Shopping list: one list both phones share'],
        can_do: ['Add, tick off and remove items'],
        accepted: [{ id: 'A1', text: 'Adding items needs internet.', changed: true }],
      },
    });
    const rowWith = (text: string) => screen.getAllByRole('row').find((r) => within(r).queryByText(text) !== null)!;

    it('opens with the plain summary, above the findings across the blueprint', () => {
      show(withCross(plain));
      const build = screen.getByText('Shopping list: one list both phones share');
      const cross = screen.getByRole('heading', { name: /^Across the blueprint/ });
      expect(build.compareDocumentPosition(cross) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(screen.getByText('Add, tick off and remove items').compareDocumentPosition(cross) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });

    it('lists the consequences you accepted, marking one whose decision changed since', () => {
      show(plain);
      const changed = screen.getAllByRole('row').filter((r) => within(r).queryByText(CHANGED_SINCE_ACCEPTED) !== null);
      expect(changed).toHaveLength(1);
      expect(changed[0]).toHaveTextContent('A1');
      expect(changed[0]).toHaveTextContent('Adding items needs internet.');
    });

    it('puts each consequence under its technical decision, with who accepted it beside the unchanged status', () => {
      show(plain);
      const row = rowWith('Items sync through a hosted database.');
      expect(within(row).getByText('Adding items needs internet.')).toBeInTheDocument();
      expect(row).toHaveTextContent(status('assumed'));
      expect(row).toHaveTextContent(acceptedLabel({ by: 'human', round: 2 }));
      expect(row).not.toHaveTextContent(status('confirmed'));
      expect(row).not.toHaveTextContent(CHANGED_SINCE_ACCEPTED);
    });

    it('keeps a superseded responder answer, marked with the answer that replaced it', () => {
      show(plain);
      const row = rowWith('Offline edits are dropped.');
      expect(row).toHaveTextContent(supersededLabel('Q7'));
      expect(row).toHaveTextContent(status('assumed'));
    });

    it('draws nothing for an empty summary', () => {
      const before = show(SIGNOFF_V1);
      const headings = screen.getAllByRole('heading', { level: 3 }).length;
      before.unmount();
      show(signoffWith({ plain_summary: { build: [], can_do: [], accepted: [] } }));
      expect(screen.getAllByRole('heading', { level: 3 })).toHaveLength(headings);
    });
  });
});

describe('SignoffSheet callouts', () => {
  const signoffWith = (over: object) => ({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), ...over } });

  it('boxes the steps and the notices apart, with code spans as code', () => {
    show(signoffWith({ steps: ['Set verify command `npm test` on project shop.'], notices: ['The agent picks a verify command.'] }));
    const steps = sectionOf(/^Before the cards run/);
    expect(steps).toHaveClass('bp-callout--steps');
    expect(within(steps).getByText('npm test').tagName).toBe('CODE');
    const notices = sectionOf(/^Notices/);
    expect(notices).toHaveClass('bp-callout--notices');
    expect(within(notices).getByText('The agent picks a verify command.')).toBeInTheDocument();
    expect(within(steps).queryByText('The agent picks a verify command.')).not.toBeInTheDocument();
  });

  it('shows no notices box without notices', () => {
    show(SIGNOFF_V1);
    expect(screen.queryByRole('heading', { name: /^Notices/ })).not.toBeInTheDocument();
  });
});

describe('BoardSheet', () => {
  it('lists the created cards, links only safe URLs and shows the total cost', () => {
    show(BOARD_V1);
    expect(screen.getByText('HOMELAND-012')).toBeInTheDocument();
    const links = screen.getAllByRole('link');
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      'https://github.com/example/homelander/pull/7',
      'https://github.com/example/homelander/compare/main...cm',
    ]);
    expect(screen.queryByText(/javascript:/)).not.toBeInTheDocument();
    expect(screen.getByText(/\$0\.41/)).toBeInTheDocument();
    const unseen = screen.getByRole('article', { name: 'HOMELAND-014 Document both endpoints' });
    expect(within(unseen).getByText('not seen yet')).toBeInTheDocument();
    const manual = sectionOf(/^Manual checks after the run/);
    expect(within(manual).getByText('curl prints one key per line.')).toBeInTheDocument();
  });

  it('boxes the setup steps and the notices', () => {
    show({ ...BOARD_V1, data: { ...(BOARD_V1.data as object), steps: ['Push an initial commit.'], notices: ['N.'] } });
    expect(sectionOf(/^Setup steps/)).toHaveClass('bp-callout--steps');
    expect(sectionOf(/^Notices/)).toHaveClass('bp-callout--notices');
  });

  it('says when some runs were unpriced', () => {
    show({ ...BOARD_V1, data: { ...(BOARD_V1.data as object), priced: false } });
    expect(screen.getByText(/some runs unpriced/)).toBeInTheDocument();
  });
});

describe('ArtifactRenderer', () => {
  it('renders an unknown stage as its body', () => {
    show({ stage: 'threat_model', version: 1, body: 'Threats.' });
    expect(screen.getByTestId('md')).toHaveTextContent('Threats.');
  });
});

describe('ReadinessSheet revision clouds', () => {
  const previous: BlueprintArtifact = {
    ...READINESS_V2,
    version: 1,
    data: {
      cards: [
        { id: 'D1', ready: true, findings: [] },
        { id: 'D3', ready: false, findings: ['An older finding.'] },
      ],
    },
  };
  const rowFor = (container: HTMLElement, id: string) => within(container).getByText(id, { selector: '.bp-id' }).closest('li')!;

  it('clouds the card whose readiness changed and no other', () => {
    const { container } = render(<ArtifactRenderer artifact={READINESS_V2} previous={previous} mode="clouds" view={view} />);
    expect(rowFor(container, 'D3').querySelector('.bp-cloud')).not.toBeNull();
    expect(rowFor(container, 'D1').querySelector('.bp-cloud')).toBeNull();
    expect(screen.getByText('Changed in rev 2.')).toBeInTheDocument();
  });

  it('clouds the finding across the blueprint that changed', () => {
    const before = withCross(previous, { cross: [CROSS[0], { ...CROSS[1], text: 'Older.' }, CROSS[2]] });
    render(<ArtifactRenderer artifact={withCross(READINESS_V2)} previous={before} mode="clouds" view={view} />);
    const table = sectionOf(/^Across the blueprint/);
    expect(within(table).getByText('DEC2 keeps field order but D3 sorts keys.').closest('.bp-cloud')).not.toBeNull();
    expect(within(table).getByText('No criterion proves O2.').closest('.bp-cloud')).toBeNull();
  });

  it('marks nothing in plain mode', () => {
    const { container } = render(<ArtifactRenderer artifact={READINESS_V2} previous={previous} mode="plain" view={view} />);
    expect(container.querySelector('.bp-cloud')).toBeNull();
  });
});

