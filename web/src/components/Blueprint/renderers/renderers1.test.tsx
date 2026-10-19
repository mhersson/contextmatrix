import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { NeedSheet } from './NeedSheet';
import { SlicesSheet } from './SlicesSheet';
import { DesignSheet } from './DesignSheet';
import { SharedDecisionsSheet } from './SharedDecisionsSheet';
import { ArtifactRenderer } from './ArtifactRenderer';
import { GroundingSheet } from './GroundingSheet';
import { AcceptanceSheet } from './AcceptanceSheet';
import {
  ACCEPTANCE_V1,
  DESIGN_V1,
  DESIGN_V2,
  GROUNDING_V1,
  NEED_V1,
  SHARED_V1,
  SHARED_V2,
  SLICES_V1,
  viewAtDesignGate,
} from '../../../test-utils/blueprintFixtures';

vi.mock('../../ChatPanel/ChatMarkdown', () => ({
  ChatMarkdown: ({ source }: { source: string }) => <div data-testid="md">{source}</div>,
}));

const view = viewAtDesignGate();

const clouded = (text: string | RegExp) => screen.getByText(text).closest('.bp-cloud') !== null;
const rowOf = (text: string | RegExp) => screen.getByText(text).closest('tr')!;
const cellsOf = (text: string | RegExp) => within(rowOf(text)).getAllByRole('cell').map((c) => c.textContent);
const revised = (text: string | RegExp, version: number) => within(rowOf(text)).queryByText(new RegExp(`rev ${version}`)) !== null;

describe('NeedSheet', () => {
  it('renders the body and a row per assumption, numbered in order, with its basis', () => {
    render(<NeedSheet artifact={NEED_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getAllByTestId('md')[0]).toHaveTextContent('Problem');
    expect(cellsOf('Clients ignore whitespace.')).toEqual(['1', 'Clients ignore whitespace.', '']);
    expect(cellsOf('Two-space indentation.')).toEqual(['2', 'Two-space indentation.', 'Q1']);
  });

  it('clouds the assumption that changed and no other', () => {
    const data = NEED_V1.data as { assumptions: unknown[] };
    const previous = { ...NEED_V1, data: { ...data, assumptions: [data.assumptions[0], { text: 'Two-space indentation.', basis: 'Q2' }] } };
    render(<NeedSheet artifact={{ ...NEED_V1, version: 2 }} previous={previous} mode="clouds" view={view} />);
    expect(revised('Two-space indentation.', 2)).toBe(true);
    expect(revised('Clients ignore whitespace.', 2)).toBe(false);
  });
});

describe('SlicesSheet', () => {
  const slices = [
    { id: 'SL2', title: 'Subcommand dispatch', value: 'Serve stays the default.', projects: ['homelander'], depends_on: [] },
    { id: 'SL1', title: 'Metrics store', value: 'Samples land on disk.', projects: ['homelander', 'docs'], depends_on: ['SL2'] },
  ];
  const assumptions = [
    { id: 'A11', text: 'An unknown subcommand exits 2.', basis: 'decided by the product owner', status: 'unconfirmed, awaiting user confirmation' },
  ];
  const sliced = (over: object = {}) => ({ ...SLICES_V1, data: { slices, assumptions, ...over } });

  it('gives each slice a row with what it delivers, its projects and the slices it comes after', () => {
    render(<SlicesSheet artifact={sliced()} previous={null} mode="plain" view={view} />);
    expect(cellsOf('Metrics store')).toEqual(['SL1', 'Metrics storeSamples land on disk.', 'homelanderdocs', 'SL2']);
    expect(cellsOf('Subcommand dispatch')[3]).toBe('');
  });

  it('gives each assumption a row with its basis under the text and its status', () => {
    render(<SlicesSheet artifact={sliced()} previous={null} mode="plain" view={view} />);
    const row = rowOf('An unknown subcommand exits 2.');
    expect(within(row).getAllByRole('cell')[0]).toHaveTextContent('A11');
    expect(within(row).getByText('decided by the product owner')).toBeInTheDocument();
    expect(within(row).getByText('unconfirmed')).toBeInTheDocument();
  });

  it('clouds the slice and the assumption that changed and no other', () => {
    const previous = sliced({
      slices: [slices[0], { ...slices[1], depends_on: [] }],
      assumptions: [{ ...assumptions[0], status: 'unverified' }],
    });
    render(<SlicesSheet artifact={{ ...sliced(), version: 2 }} previous={previous} mode="clouds" view={view} />);
    expect(revised('Metrics store', 2)).toBe(true);
    expect(revised('Subcommand dispatch', 2)).toBe(false);
    expect(revised('An unknown subcommand exits 2.', 2)).toBe(true);
  });

  it('clouds nothing when an unchanged version repeats an assumption id', () => {
    const repeated = sliced({ assumptions: [assumptions[0], { ...assumptions[0], text: 'A second sentence under the same id.' }] });
    render(<SlicesSheet artifact={{ ...repeated, version: 2 }} previous={repeated} mode="clouds" view={view} />);
    expect(revised('An unknown subcommand exits 2.', 2)).toBe(false);
    expect(revised('A second sentence under the same id.', 2)).toBe(false);
  });

  it('keeps the slices of an older blueprint without assumptions', () => {
    render(<SlicesSheet artifact={SLICES_V1} previous={null} mode="plain" view={view} />);
    expect(cellsOf('Pretty-print GET /')).toEqual(['SL1', 'Pretty-print GET /Readable with plain curl.', 'homelander', '']);
    expect(screen.queryByRole('heading', { name: /Assumptions/ })).not.toBeInTheDocument();
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

  it('heads a one-slice design with its projects only', () => {
    render(<DesignSheet artifact={DESIGN_V2} previous={null} mode="plain" view={view} />);
    expect(screen.queryByText(/SL1/)).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'homelander' })).toBeInTheDocument();
  });

  it('heads each slice with its id when the design holds several', () => {
    const data = DESIGN_V2.data as { slices: Record<string, unknown>[] };
    const two = { ...data, slices: [data.slices[0], { ...data.slices[0], id: 'SL2', projects: ['web'], decisions: [] }] };
    render(<DesignSheet artifact={{ ...DESIGN_V2, data: two }} previous={null} mode="plain" view={view} />);
    expect(screen.getByRole('heading', { name: 'SL1 homelander' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'SL2 web' })).toBeInTheDocument();
  });

  it('draws no heading for one slice without projects', () => {
    const data = DESIGN_V2.data as { slices: Record<string, unknown>[] };
    const bare = { ...data, slices: [{ ...data.slices[0], projects: [] }] };
    render(<DesignSheet artifact={{ ...DESIGN_V2, data: bare }} previous={null} mode="plain" view={view} />);
    expect(screen.queryByText(/SL1/)).not.toBeInTheDocument();
    expect(screen.getByText('Indent through the existing encoder')).toBeInTheDocument();
  });

  it('clouds the decision that changed and no other', () => {
    const { container } = render(<DesignSheet artifact={DESIGN_V2} previous={DESIGN_V1} mode="clouds" view={view} />);
    const decisionClouds = [...container.querySelectorAll('.bp-cloud')].filter((c) => c.querySelector('.bp-dec'));
    expect(decisionClouds).toHaveLength(1);
    expect(decisionClouds[0]).toHaveTextContent('DEC1');
  });

  it('adds the design assumptions as rows with their basis only when there are any', () => {
    const { unmount } = render(<DesignSheet artifact={DESIGN_V2} previous={null} mode="plain" view={view} />);
    expect(screen.queryByRole('heading', { name: /Assumptions/ })).not.toBeInTheDocument();
    unmount();
    const assumptions = [{ text: 'A bad store line names its number.', basis: 'Q16' }];
    render(<DesignSheet artifact={{ ...DESIGN_V2, data: { ...(DESIGN_V2.data as object), assumptions } }} previous={null} mode="plain" view={view} />);
    expect(screen.getByRole('heading', { name: /Assumptions/ })).toBeInTheDocument();
    expect(cellsOf('A bad store line names its number.')).toEqual(['1', 'A bad store line names its number.', 'Q16']);
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
  it('gives each slice a table of its criteria with their project', () => {
    render(<AcceptanceSheet artifact={ACCEPTANCE_V1} previous={null} mode="plain" view={view} />);
    const slice = screen.getByRole('heading', { name: /^SL1/ }).closest('section')!;
    expect(within(slice).getAllByRole('row')).toHaveLength(3);
    expect(cellsOf('The body has several lines.')).toEqual(['AC1', 'The body has several lines.', 'homelander']);
  });

  it('gives each manual check a row naming its slice', () => {
    render(<AcceptanceSheet artifact={ACCEPTANCE_V1} previous={null} mode="plain" view={view} />);
    const manual = screen.getByRole('heading', { name: /^Manual checks/ }).closest('section')!;
    expect(within(manual).getByText('curl prints one key per line.')).toBeInTheDocument();
    expect(cellsOf('curl prints one key per line.')).toEqual(['MC1', 'SL1', 'curl prints one key per line.']);
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

describe('SharedDecisionsSheet', () => {
  it('renders the shared decisions, contracts and assumptions as one unsliced section', () => {
    render(<SharedDecisionsSheet artifact={SHARED_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getByText('Deploy tooling')).toBeInTheDocument();
    expect(screen.getByText(/Kustomize overlays per environment/)).toBeInTheDocument();
    expect(screen.getByText('Rejected: Helm chart (templates are hard to review)')).toBeInTheDocument();
    expect(screen.getByText('Every service logs JSON to stdout.')).toBeInTheDocument();
    expect(screen.getByText('One namespace per environment.').closest('table')).not.toBeNull();
    expect(screen.queryByRole('heading', { name: /^SL\d/ })).not.toBeInTheDocument();
  });

  it('clouds the changed and new decisions only', () => {
    const { container } = render(<SharedDecisionsSheet artifact={SHARED_V2} previous={SHARED_V1} mode="clouds" view={view} />);
    const clouded = [...container.querySelectorAll('.bp-cloud')].filter((c) => c.querySelector('.bp-dec'));
    expect(clouded.map((c) => c.querySelector('.bp-id')?.textContent)).toEqual(['DEC1', 'DEC3']);
  });

  it('diffs a changed choice and strikes a removed decision in compare mode', () => {
    const { container } = render(<SharedDecisionsSheet artifact={SHARED_V2} previous={SHARED_V1} mode="compare" view={view} />);
    const dec1 = [...container.querySelectorAll('.bp-dec')].find((d) => d.textContent?.includes('DEC1'))!;
    expect(dec1.querySelector('del')).toHaveTextContent(/environment/);
    expect(dec1.querySelector('ins')).toHaveTextContent(/cluster/);
    expect(container.querySelector('.bp-dec--removed')).toHaveTextContent('DEC2 Observability stack');
  });

  it('is what the sheet draws for the shared decisions stage', () => {
    render(<ArtifactRenderer artifact={SHARED_V1} previous={null} mode="plain" view={view} />);
    expect(screen.getByText('Deploy tooling')).toBeInTheDocument();
  });
});
