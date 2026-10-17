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
