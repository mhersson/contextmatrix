import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { NeedSheet } from './NeedSheet';
import { SlicesSheet } from './SlicesSheet';
import { DesignSheet } from './DesignSheet';
import { GroundingSheet } from './GroundingSheet';
import { AcceptanceSheet } from './AcceptanceSheet';
import {
  ACCEPTANCE_V1,
  DESIGN_V1,
  DESIGN_V2,
  GROUNDING_V1,
  NEED_V1,
  SLICES_V1,
  viewAtDesignGate,
} from '../../../test-utils/blueprintFixtures';

vi.mock('../../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

const view = viewAtDesignGate();

const clouded = (text: string | RegExp) => screen.getByText(text).closest('.bp-cloud') !== null;

describe('NeedSheet', () => {
  it('renders the body and the assumptions with their basis', () => {
    render(<NeedSheet artifact={NEED_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getAllByTestId('md')[0]).toHaveTextContent('Problem');
    const item = screen.getByText('Two-space indentation.').closest('li')!;
    expect(within(item).getByText('Q1')).toBeInTheDocument();
    expect(screen.getByText('Clients ignore whitespace.')).toBeInTheDocument();
  });
});

describe('SlicesSheet', () => {
  it('lists each slice with its value and projects', () => {
    render(<SlicesSheet artifact={SLICES_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getByText('Pretty-print GET /')).toBeInTheDocument();
    expect(screen.getByText('Readable with plain curl.')).toBeInTheDocument();
    expect(screen.getByText('homelander')).toBeInTheDocument();
  });
});

describe('DesignSheet', () => {
  it('renders decisions with chosen, rejected options, basis chips and contracts', () => {
    render(<DesignSheet artifact={DESIGN_V2} previous={null} mode="plain" view={view} />);
    expect(screen.getByText('Indent through the existing encoder')).toBeInTheDocument();
    expect(screen.getByText(/Call SetIndent on the encoder/)).toBeInTheDocument();
    expect(screen.getByText('Rejected: Middleware for every route (out of scope)')).toBeInTheDocument();
    expect(screen.getByText('Q1')).toBeInTheDocument();
    expect(screen.getByText('S1')).toBeInTheDocument();
    expect(screen.getByText('None between projects.')).toBeInTheDocument();
  });

  it('clouds the decision that changed and no other', () => {
    const { container } = render(<DesignSheet artifact={DESIGN_V2} previous={DESIGN_V1} mode="clouds" view={view} />);
    const decisionClouds = [...container.querySelectorAll('.bp-cloud')].filter((c) => c.querySelector('.bp-dec'));
    expect(decisionClouds).toHaveLength(1);
    expect(decisionClouds[0]).toHaveTextContent('DEC1');
  });

  it('diffs the chosen option word by word in compare mode', () => {
    const { container } = render(<DesignSheet artifact={DESIGN_V2} previous={DESIGN_V1} mode="compare" view={view} />);
    const dec1 = [...container.querySelectorAll('.bp-dec')].find((d) => d.textContent?.includes('DEC1'))!;
    expect(dec1.querySelector('del')).toHaveTextContent(/MarshalIndent/);
    expect(dec1.querySelector('ins')).toHaveTextContent(/SetIndent/);
  });
});

describe('GroundingSheet', () => {
  it('shows each checkout with its verify command and the verdicts with sources', () => {
    render(<GroundingSheet artifact={GROUNDING_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getByText('b754015')).toBeInTheDocument();
    expect(screen.getByText('empty repository')).toBeInTheDocument();
    expect(screen.getByText('go test ./...')).toBeInTheDocument();
    expect(screen.getByText('make check')).toBeInTheDocument();
    const verified = screen.getByRole('heading', { name: 'Verified' }).closest('section')!;
    expect(within(verified).getByText(/Encode already ends the body/)).toBeInTheDocument();
    expect(within(verified).getByText('S1')).toBeInTheDocument();
    const contradicted = screen.getByRole('heading', { name: 'Contradicted' }).closest('section')!;
    expect(within(contradicted).getByText(/README has no example/)).toBeInTheDocument();
  });

  it('folds the written grounding under the data and opens it in compare mode', () => {
    const { container, rerender } = render(<GroundingSheet artifact={GROUNDING_V1} previous={null} mode="plain" view={view} />);
    const details = container.querySelector('details')!;
    expect(details.open).toBe(false);
    expect(details).toHaveTextContent('The handler is the only place');
    expect(details.previousElementSibling).not.toBeNull();
    rerender(<GroundingSheet artifact={GROUNDING_V1} previous={GROUNDING_V1} mode="compare" view={view} />);
    expect(container.querySelector('details')!.open).toBe(true);
  });

  describe('revision clouds', () => {
    const next = GROUNDING_V1;
    const data = next.data as { projects: Record<string, object>; verdicts: { id: string; status: string; evidence: string }[] };
    const previousWith = (over: object): typeof next => ({ ...next, version: 0, data: { ...data, ...over } });
    const nextV2 = { ...next, version: 2 };

    it('clouds the verdict that changed and no other', () => {
      const previous = previousWith({ verdicts: [data.verdicts[0], { ...data.verdicts[1], evidence: '[S2] Older evidence.' }] });
      render(<GroundingSheet artifact={nextV2} previous={previous} mode="clouds" view={view} />);
      expect(clouded(/README has no example/)).toBe(true);
      expect(clouded(/Encode already ends/)).toBe(false);
      expect(screen.getByText('Changed in rev 2.')).toBeInTheDocument();
    });

    it('clouds the checkout table when a project changed', () => {
      const previous = previousWith({ projects: { ...data.projects, emptyrepo: { sha: 'abc' } } });
      const { container } = render(<GroundingSheet artifact={nextV2} previous={previous} mode="clouds" view={view} />);
      expect(container.querySelector('table')!.closest('.bp-cloud')).not.toBeNull();
    });

    it('marks nothing in plain mode or when nothing changed', () => {
      const previous = previousWith({ verdicts: [data.verdicts[0]] });
      const plain = render(<GroundingSheet artifact={nextV2} previous={previous} mode="plain" view={view} />);
      expect(plain.container.querySelector('.bp-cloud')).toBeNull();
      plain.unmount();
      const same = render(<GroundingSheet artifact={nextV2} previous={{ ...next, version: 1 }} mode="clouds" view={view} />);
      expect(same.container.querySelector('.bp-cloud')).toBeNull();
    });
  });

  it('renders the body plainly when there is no data', () => {
    const { container } = render(<GroundingSheet artifact={{ ...GROUNDING_V1, data: undefined }} previous={null} mode="plain" view={view} />);
    expect(container.querySelector('details')).toBeNull();
    expect(screen.getByTestId('md')).toHaveTextContent('The handler is the only place');
  });
});

describe('AcceptanceSheet', () => {
  it('groups criteria by slice and project and lists manual checks', () => {
    render(<AcceptanceSheet artifact={ACCEPTANCE_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getByRole('heading', { name: 'SL1' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'homelander' })).toBeInTheDocument();
    expect(screen.getByText('The body has several lines.')).toBeInTheDocument();
    const manual = screen.getByRole('heading', { name: 'Manual checks' }).closest('section')!;
    expect(within(manual).getByText('curl prints one key per line.')).toBeInTheDocument();
  });
});

describe('AcceptanceSheet revision clouds', () => {
  const data = ACCEPTANCE_V1.data as { slices: { id: string; criteria: { id: string; project: string; text: string }[]; manual: { id: string; text: string }[] }[] };
  const slice = data.slices[0];
  const artifact = { ...ACCEPTANCE_V1, version: 2 };
  const previousWith = (over: object) => ({ ...ACCEPTANCE_V1, version: 1, data: { slices: [{ ...slice, ...over }] } });

  it('clouds the criterion that changed and no other', () => {
    const previous = previousWith({ criteria: [slice.criteria[0], { ...slice.criteria[1], text: 'Older.' }] });
    render(<AcceptanceSheet artifact={artifact} previous={previous} mode="clouds" view={view} />);
    expect(clouded('Keys keep struct order.')).toBe(true);
    expect(clouded('The body has several lines.')).toBe(false);
    expect(screen.getByText('Changed in rev 2.')).toBeInTheDocument();
  });

  it('clouds a manual check that changed', () => {
    const previous = previousWith({ manual: [{ id: 'MC1', text: 'Older.' }] });
    render(<AcceptanceSheet artifact={artifact} previous={previous} mode="clouds" view={view} />);
    expect(clouded('curl prints one key per line.')).toBe(true);
    expect(clouded('The body has several lines.')).toBe(false);
  });

  it('marks nothing in plain mode', () => {
    const previous = previousWith({ manual: [{ id: 'MC1', text: 'Older.' }] });
    const { container } = render(<AcceptanceSheet artifact={artifact} previous={previous} mode="plain" view={view} />);
    expect(container.querySelector('.bp-cloud')).toBeNull();
  });
});

