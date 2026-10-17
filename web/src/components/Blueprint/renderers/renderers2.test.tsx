import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { ArtifactRenderer } from './ArtifactRenderer';
import { BOARD_V1, CARDS_V1, READINESS_V2, SIGNOFF_V1, ref, viewAtDesignGate } from '../../../test-utils/blueprintFixtures';
import type { BlueprintArtifact } from '../../../types';

vi.mock('../../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

const view = viewAtDesignGate();
const show = (artifact: BlueprintArtifact) =>
  render(<ArtifactRenderer artifact={artifact} previous={null} mode="plain" view={view} />);

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

  it('shows each manual check once when the playbook order already lists it', () => {
    show(CARDS_V1);
    expect(screen.getAllByText('M1', { selector: '.bp-id' })).toHaveLength(1);
    expect(screen.queryByRole('heading', { name: 'Manual checks' })).not.toBeInTheDocument();
  });

  it('keeps a manual check missing from the order under Manual checks without repeating the ordered ones', () => {
    const data = CARDS_V1.data as { manual: { id: string; text: string; criteria: string[] }[] };
    show({
      ...CARDS_V1,
      data: { ...data, manual: [...data.manual, { id: 'M2', text: 'Open the health page.', criteria: [] }] },
    });
    const checks = screen.getByRole('heading', { name: 'Manual checks' }).closest('section')!;
    expect(within(checks).getByText('M2')).toBeInTheDocument();
    expect(within(checks).queryByText('M1')).not.toBeInTheDocument();
    expect(screen.getAllByText('M1', { selector: '.bp-id' })).toHaveLength(1);
  });

  it('shows the manual checks under Manual checks for a single card', () => {
    const data = CARDS_V1.data as { cards: unknown[]; manual: { id: string; text: string }[] };
    show({ ...CARDS_V1, data: { ...data, cards: data.cards.slice(0, 1), order: ['D1', 'M1'] } });
    expect(screen.queryByRole('heading', { name: /Playbook order/ })).not.toBeInTheDocument();
    const checks = screen.getByRole('heading', { name: 'Manual checks' }).closest('section')!;
    expect(within(checks).getByText('Run curl against the server.')).toBeInTheDocument();
  });
});

describe('ReadinessSheet', () => {
  function inLoop() {
    const v = viewAtDesignGate();
    v.pipeline = v.pipeline.map((s) => (s.stage === 'readiness' ? { ...s, status: 'running' as const, round: 2, max_rounds: 3 } : s));
    v.artifacts = [...v.artifacts, ref('readiness', 1, 'superseded'), ref('readiness', 2, 'current')];
    return v;
  }

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
  it('shows the card order, assumption chips, warnings, manual checks and setup steps', () => {
    show(SIGNOFF_V1);
    expect(screen.getByText('Pretty-print the GET / response')).toBeInTheDocument();
    for (const chip of ['unverified', 'confirmed by you (Q1)', 'assumed by responder (Q3)', 'verified S1', 'contradicted']) {
      expect(screen.getByText(chip)).toBeInTheDocument();
    }
    expect(screen.getByText('One assumption is unverified.')).toBeInTheDocument();
    const manual = screen.getByRole('heading', { name: 'Manual checks after the run' }).closest('section')!;
    expect(within(manual).getByText('curl prints one key per line.')).toBeInTheDocument();
    expect(screen.getByText(/set verify command/)).toBeInTheDocument();
  });

  it('falls back to prose assumptions from an older blueprint', () => {
    show({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), assumptions: 'A1 holds.' } });
    expect(screen.getByText('A1 holds.')).toBeInTheDocument();
  });

  it('says when the preflight could not run', () => {
    show({ ...SIGNOFF_V1, data: { ...(SIGNOFF_V1.data as object), preflight_error: 'repo unreachable' } });
    expect(screen.getByRole('note', { name: 'Preflight' })).toHaveTextContent('repo unreachable');
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
    const manual = screen.getByRole('heading', { name: 'Manual checks after the run' }).closest('section')!;
    expect(within(manual).getByText('curl prints one key per line.')).toBeInTheDocument();
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
