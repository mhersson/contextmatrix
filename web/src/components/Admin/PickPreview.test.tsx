import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import type { SelectorLadders } from '../../types';
import { PickPreview } from './PickPreview';
import { CANDIDATES, DEFAULT_BARS, pick, previewFixture, seat } from './selector.fixtures';

const LADDERS: SelectorLadders = { coder: { ...DEFAULT_BARS }, reviewer: { ...DEFAULT_BARS } };
const BASE = {
  ladders: LADDERS,
  separate: false,
  candidates: CANDIDATES,
  blacklist: new Set<string>(),
  headroom: 1.5,
  selectedTier: 'complex' as const,
  onSelectTier: () => {},
};

describe('PickPreview', () => {
  it('renders picks, prices, the descended chip, walked seats and clearing counts', () => {
    render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} />);

    const complexTier = screen.getByTestId('tl-pv-complex');
    expect(within(complexTier).getByText('2 coder')).toBeInTheDocument();
    expect(within(complexTier).getByText('3 reviewer')).toBeInTheDocument();
    expect(within(complexTier).getAllByText('cheap')).toHaveLength(3);
    expect(within(complexTier).queryByText(/↓/)).toBeNull();
    expect(within(complexTier).getAllByText('$2.0/M').length).toBeGreaterThanOrEqual(2);
    expect(screen.getByTestId('tl-seat-complex-1')).toHaveClass('walk');
    expect(screen.getByTestId('tl-seat-complex-1')).toHaveTextContent('walked');
    expect(screen.getByTestId('tl-seat-complex-0')).not.toHaveClass('walk');
    expect(within(complexTier).getByText('$26.0/M')).toBeInTheDocument();

    const critical = screen.getByTestId('tl-pv-critical');
    expect(within(critical).getByText('↓ complex')).toBeInTheDocument();
    expect(within(critical).getByText('no seat can be filled')).toBeInTheDocument();
    expect(screen.getByText('headroom 1.5× · favorites and blacklist applied')).toBeInTheDocument();
  });

  it('right-clicking a pick name or a seat reports the slug and pointer', () => {
    const onModelMenu = vi.fn();
    render(
      <PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} onModelMenu={onModelMenu} />,
    );

    const complexTier = screen.getByTestId('tl-pv-complex');
    const [coderName] = within(complexTier).getAllByText('cheap');
    expect(fireEvent.contextMenu(coderName, { clientX: 10, clientY: 20 })).toBe(false);
    expect(onModelMenu).toHaveBeenLastCalledWith('a/cheap', 10, 20);

    expect(fireEvent.contextMenu(screen.getByTestId('tl-seat-complex-1'), { clientX: 30, clientY: 40 })).toBe(false);
    expect(onModelMenu).toHaveBeenLastCalledWith('b/pricey', 30, 40);
    expect(onModelMenu).toHaveBeenCalledTimes(2);
  });

  it('gives a seat with no model no menu', () => {
    const onModelMenu = vi.fn();
    const p = previewFixture();
    p.tiers.complex.panel[2] = seat('', 0, false, 'complex');
    render(<PickPreview preview={p} pending={false} disabled={false} error={null} {...BASE} onModelMenu={onModelMenu} />);

    expect(fireEvent.contextMenu(screen.getByTestId('tl-seat-complex-2'), { clientX: 1, clientY: 2 })).toBe(true);
    expect(onModelMenu).not.toHaveBeenCalled();
  });

  it('leaves the browser menu alone when no handler is wired', () => {
    render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} />);

    expect(fireEvent.contextMenu(screen.getByTestId('tl-seat-complex-1'), { clientX: 1, clientY: 2 })).toBe(true);
  });

  it('says so when nothing clears any rung', () => {
    const p = previewFixture();
    p.tiers.critical.coder = pick('', 'coder', 'critical', '', 0);
    render(<PickPreview preview={p} pending={false} disabled={false} error={null} {...BASE} />);

    expect(within(screen.getByTestId('tl-pv-critical')).getByText('nothing clears any rung')).toBeInTheDocument();
  });

  it('marks the body busy while a preview is pending', () => {
    render(<PickPreview preview={previewFixture()} pending disabled={false} error={null} {...BASE} />);

    expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByText('computing…')).toBeInTheDocument();
  });

  it('keeps the last good preview under an error line', () => {
    render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error="Preview failed." {...BASE} />);

    expect(screen.getByRole('alert')).toHaveTextContent('Preview failed.');
    expect(screen.getByTestId('tl-seat-complex-0')).toBeInTheDocument();
  });

  it('shows an empty state before the first preview', () => {
    render(<PickPreview preview={null} pending disabled={false} error={null} {...BASE} />);

    expect(screen.getByText('Waiting for the first preview…')).toBeInTheDocument();
  });

  it('promises nothing while disabled: no wait, no busy, no computing', () => {
    render(<PickPreview preview={null} pending disabled error={null} {...BASE} />);

    expect(screen.getByText('Preview needs the candidate catalog.')).toBeInTheDocument();
    expect(screen.queryByText('Waiting for the first preview…')).not.toBeInTheDocument();
    expect(screen.queryByText('computing…')).not.toBeInTheDocument();
    expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'false');
  });

  it('marks a pick, a seat and the panel total as list price when the price is the AA list price', () => {
    const p = previewFixture();
    p.tiers.complex.coder = pick('a/cheap', 'coder', 'complex', 'complex', 2e-6, { price_source: 'aa' });
    p.tiers.complex.panel[1] = seat('b/pricey', 2e-5, true, 'complex', { price_source: 'aa' });
    render(<PickPreview preview={p} pending={false} disabled={false} error={null} {...BASE} />);

    const complexTier = screen.getByTestId('tl-pv-complex');
    // The coder pick, seat 1 and the panel total; the reviewer pick stays unmarked.
    expect(within(complexTier).getAllByText('list')).toHaveLength(3);
    expect(within(screen.getByTestId('tl-seat-complex-1')).getByText('list')).toHaveAttribute('title', expect.stringContaining('list price'));
    expect(within(screen.getByTestId('tl-pv-simple')).queryByText('list')).toBeNull();
  });

  it('clicking a tier row selects it, and aria-selected follows selectedTier', () => {
    const onSelectTier = vi.fn();
    const { rerender } = render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} onSelectTier={onSelectTier} />);

    expect(screen.getByTestId('tl-pv-complex')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('tl-pv-moderate')).toHaveAttribute('aria-selected', 'false');
    fireEvent.click(within(screen.getByTestId('tl-pv-moderate')).getByRole('button', { name: 'moderate' }));
    expect(onSelectTier).toHaveBeenLastCalledWith('moderate');
    fireEvent.click(within(screen.getByTestId('tl-pv-simple')).getByText('4 clear'));
    expect(onSelectTier).toHaveBeenLastCalledWith('simple');

    rerender(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} selectedTier="moderate" onSelectTier={onSelectTier} />);
    expect(screen.getByTestId('tl-pv-moderate')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('tl-pv-complex')).toHaveAttribute('aria-selected', 'false');
    expect(within(screen.getByTestId('tl-pv-moderate')).getByRole('button', { name: 'moderate' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('counts candidates per role, leaving out blacklisted ones', () => {
    const { rerender } = render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} />);
    // complex: 2 coders and 3 reviewers clear; simple: all of a/*, b/pricey and c/weak (0.7) clear for both.
    expect(within(screen.getByTestId('tl-pv-complex')).getByText('2 coder')).toBeInTheDocument();
    expect(within(screen.getByTestId('tl-pv-complex')).getByText('3 reviewer')).toBeInTheDocument();
    expect(within(screen.getByTestId('tl-pv-simple')).getByText('4 clear')).toBeInTheDocument();

    rerender(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} blacklist={new Set(['c/weak', 'a/mid'])} />);
    expect(within(screen.getByTestId('tl-pv-simple')).getByText('2 clear')).toBeInTheDocument();
    expect(within(screen.getByTestId('tl-pv-complex')).getByText('2 clear')).toBeInTheDocument();
  });

  it('shows both bars only when the ladders differ at that tier', () => {
    const ladders: SelectorLadders = { coder: { ...DEFAULT_BARS }, reviewer: { ...DEFAULT_BARS, complex: 0.84 } };
    const { rerender } = render(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} ladders={ladders} separate />);

    const complex = screen.getByTestId('tl-pv-complex');
    expect(within(complex).getByText('c 0.82')).toBeInTheDocument();
    expect(within(complex).getByText('r 0.84')).toBeInTheDocument();
    expect(within(screen.getByTestId('tl-pv-simple')).queryByText(/^c /)).toBeNull();
    expect(within(screen.getByTestId('tl-pv-simple')).getByText('0.65')).toBeInTheDocument();

    rerender(<PickPreview preview={previewFixture()} pending={false} disabled={false} error={null} {...BASE} ladders={ladders} separate={false} />);
    expect(within(screen.getByTestId('tl-pv-complex')).queryByText('r 0.84')).toBeNull();
  });
});
