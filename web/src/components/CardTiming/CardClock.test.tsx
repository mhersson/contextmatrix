import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Card } from '../../types';
import { CardClock, CardTimingDetails } from './CardClock';
import { formatStamp } from '../../lib/cardTiming';

const T0 = Date.parse('2026-01-01T12:00:00Z');
const iso = (offsetMin: number) => new Date(T0 + offsetMin * 60_000).toISOString();
// Stamps render in the viewer's locale and time zone.
const at = (offsetMin: number) => formatStamp(iso(offsetMin));

// In progress since 11:00, claimed at 11:46, heartbeat at 12:00 (now).
const worked: Card = {
  id: 'TEST-001',
  title: 'Sort the spice rack',
  project: 'test',
  type: 'task',
  state: 'in_progress',
  priority: 'medium',
  created: iso(-120),
  updated: iso(0),
  body: '',
  autonomous: false,
  state_since: iso(-60),
  assigned_agent: 'worker-7',
  working_since: iso(-14),
  last_heartbeat: iso(0),
  worked_seconds: 0,
  work_intervals: [
    { agent: 'worker-3', from: iso(-50), to: iso(-30) },
    { agent: 'worker-7', from: iso(-14), to: null },
  ],
};

const unheld: Card = {
  ...worked,
  state: 'review',
  assigned_agent: undefined,
  working_since: undefined,
  last_heartbeat: undefined,
  unclaimed_since: iso(-90),
  worked_seconds: 1200,
  work_intervals: [{ agent: 'worker-3', from: iso(-110), to: iso(-90) }],
};

const clock = () => screen.queryByRole('button', { name: /^In Progress for/ });

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(T0);
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('CardClock on the tile', () => {
  it('shows a clock icon and the time in the current state while an agent works on the card', () => {
    render(<CardClock card={worked} />);
    const button = clock()!;
    expect(button).toHaveTextContent('1h 0m');
    expect(button).toHaveAccessibleName('In Progress for 1h 0m, worker-7 working');
    expect(button.querySelector('svg')).not.toBeNull();
  });

  it.each([
    ['nobody holds it', { assigned_agent: undefined, working_since: undefined }],
    ['the claim start is unknown', { working_since: undefined }],
    ['the card is stalled', { state: 'stalled' }],
    ['the card is done (the claim is kept until release)', { state: 'done' }],
    ['the card is blocked', { state: 'blocked' }],
    ['the card is todo, even with a holder', { state: 'todo' }],
    ['the card is not planned', { state: 'not_planned' }],
  ] as const)('shows nothing when %s', (_name, patch) => {
    const { container } = render(<CardClock card={{ ...worked, ...patch } as Card} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('ticks in the browser', () => {
    render(<CardClock card={worked} />);
    act(() => { vi.advanceTimersByTime(5 * 60_000); });
    expect(clock()).toHaveTextContent('1h 5m');
  });

  it('opens a timing box on hover and closes it on leave', () => {
    render(<CardClock card={worked} />);
    const button = clock()!;
    fireEvent.mouseEnter(button);
    const box = screen.getByRole('tooltip');
    expect(button).toHaveAttribute('aria-describedby', box.id);
    expect(within(box).getByTestId('timing-state')).toHaveTextContent('In Progress for 1h 0m');
    expect(within(box).getByTestId('timing-state')).toHaveTextContent(`since ${at(-60)}`);
    expect(within(box).getByTestId('timing-holder')).toHaveTextContent('worker-7 working for 14m 0s');
    expect(within(box).getByTestId('timing-heartbeat')).toHaveTextContent('Heartbeat 0s ago');
    // 20 minutes of the first claim plus 14 of the held one.
    expect(within(box).getByTestId('timing-worked')).toHaveTextContent('Worked 34m 0s in total');
    const claims = within(box).getByTestId('timing-intervals');
    expect(claims).toHaveTextContent('2 claims');
    expect(claims).toHaveTextContent(`worker-3 ${at(-50)} → ${at(-30)} · 20m 0s`);
    expect(claims).toHaveTextContent(`worker-7 ${at(-14)} → now · 14m 0s`);
    fireEvent.mouseLeave(button);
    act(() => { vi.advanceTimersByTime(200); });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    expect(button).not.toHaveAttribute('aria-describedby');
  });

  it('stays open while the pointer rests on the box, and a press in it does not reach the card', () => {
    const onCardClick = vi.fn();
    const onCardPointerDown = vi.fn();
    render(<div onClick={onCardClick} onPointerDown={onCardPointerDown}><CardClock card={worked} /></div>);
    const button = clock()!;
    fireEvent.mouseEnter(button);
    const box = screen.getByRole('tooltip');
    expect(box).toHaveClass('card-hover--hoverable');
    fireEvent.mouseLeave(button);
    fireEvent.mouseEnter(box);
    act(() => { vi.advanceTimersByTime(1000); });
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.pointerDown(box);
    fireEvent.click(box);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    expect(onCardClick).not.toHaveBeenCalled();
    expect(onCardPointerDown).not.toHaveBeenCalled();
    fireEvent.mouseLeave(box);
    act(() => { vi.advanceTimersByTime(200); });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('opens on keyboard focus and closes on Escape without the key reaching the card or board', () => {
    const onCardKey = vi.fn();
    render(<div onKeyDown={onCardKey}><CardClock card={worked} /></div>);
    fireEvent.keyDown(document, { key: 'Tab' });
    const button = clock()!;
    act(() => { button.focus(); });
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.keyDown(button, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    expect(onCardKey).not.toHaveBeenCalled();
  });

  it('closes on blur', () => {
    render(<CardClock card={worked} />);
    fireEvent.keyDown(document, { key: 'Tab' });
    act(() => { clock()!.focus(); });
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    act(() => { clock()!.blur(); });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('opens on a click or tap, not on the focus a click gives, and the click does not open the card', () => {
    const onCardClick = vi.fn();
    render(<div onClick={onCardClick}><CardClock card={worked} /></div>);
    const button = clock()!;
    fireEvent.mouseDown(button);
    act(() => { button.focus(); });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
    fireEvent.click(button);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    // A second click (or a tap after a touch mouseenter) keeps it open.
    fireEvent.click(button);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    expect(onCardClick).not.toHaveBeenCalled();
  });

  it('keeps focus on the clock when the box is pressed, so it stays open and Escape still works', () => {
    render(<CardClock card={worked} />);
    const button = clock()!;
    act(() => { button.focus(); });
    fireEvent.click(button);
    const box = screen.getByRole('tooltip');
    // A prevented mousedown is what stops the browser moving focus off the clock.
    expect(fireEvent.mouseDown(box)).toBe(false);
    fireEvent.keyDown(button, { key: 'Escape' });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('closes on a press elsewhere, as a tap outside on a touch screen', () => {
    render(<><CardClock card={worked} /><p>elsewhere</p></>);
    fireEvent.click(clock()!);
    expect(screen.getByRole('tooltip')).toBeInTheDocument();
    fireEvent.pointerDown(screen.getByText('elsewhere'));
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('closes when the board scrolls', () => {
    render(<CardClock card={worked} />);
    fireEvent.mouseEnter(clock()!);
    act(() => { window.dispatchEvent(new Event('scroll')); });
    expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();
  });

  it('lists the newest five claims and points to the Info tab for the rest', () => {
    const many = Array.from({ length: 7 }, (_, i) => ({ agent: `worker-${i}`, from: iso(-100 + i * 10), to: iso(-95 + i * 10) }));
    render(<CardClock card={{ ...worked, work_intervals: [...many, { agent: 'worker-7', from: iso(-14), to: null }] }} />);
    fireEvent.mouseEnter(clock()!);
    const claims = screen.getByTestId('timing-intervals');
    expect(claims).toHaveTextContent('8 claims');
    expect(claims).not.toHaveTextContent('worker-2 ');
    expect(claims).toHaveTextContent('worker-3 ');
    expect(claims).toHaveTextContent("3 earlier in the card's Info tab");
  });
});

describe('CardTimingDetails in the card detail', () => {
  it('uses the timing box rows and lists every claim', () => {
    render(<CardTimingDetails card={worked} />);
    expect(screen.getByRole('heading', { name: 'Timing' })).toBeInTheDocument();
    expect(screen.getByTestId('timing-holder')).toHaveTextContent('worker-7 working for 14m 0s');
    expect(screen.getByTestId('timing-intervals')).not.toHaveTextContent('earlier');
  });

  it('shows how long nobody has held a card, and the warning only past an opted-in limit', () => {
    const { rerender } = render(<CardTimingDetails card={unheld} />);
    expect(screen.getByTestId('timing-holder')).toHaveTextContent('No agent holds it');
    expect(screen.getByTestId('timing-holder')).toHaveTextContent(`for 1h 30m, since ${at(-90)}`);
    expect(screen.queryByTestId('timing-heartbeat')).not.toBeInTheDocument();
    expect(screen.getByTestId('timing-worked')).toHaveTextContent('Worked 20m 0s in total');
    expect(screen.queryByTestId('timing-warning')).not.toBeInTheDocument();
    rerender(<CardTimingDetails card={unheld} unclaimedWarningSeconds={3600} />);
    expect(screen.getByTestId('timing-warning')).toHaveTextContent('No agent for over 1h');
    rerender(<CardTimingDetails card={unheld} unclaimedWarningSeconds={2 * 3600} />);
    expect(screen.queryByTestId('timing-warning')).not.toBeInTheDocument();
  });

  it('says the total is unknown when the server has no complete claim record', () => {
    render(<CardTimingDetails card={{ ...worked, worked_seconds: undefined, work_intervals: undefined }} />);
    expect(screen.getByTestId('timing-worked')).toHaveTextContent('Worked: unknown');
    expect(screen.queryByTestId('timing-intervals')).not.toBeInTheDocument();
  });

  it('shows a finished card with its date, and no holder row once released', () => {
    render(<CardTimingDetails card={{ ...unheld, state: 'done' }} unclaimedWarningSeconds={60} />);
    const state = screen.getByTestId('timing-state');
    expect(state).toHaveTextContent(`Shipped ${at(-60)}`);
    expect(state).not.toHaveTextContent('since');
    expect(screen.queryByTestId('timing-holder')).not.toBeInTheDocument();
    expect(screen.queryByTestId('timing-warning')).not.toBeInTheDocument();
    expect(screen.getByTestId('timing-intervals')).toBeInTheDocument();
  });

  it('keeps the holder and heartbeat rows for a finished card that is still held', () => {
    render(<CardTimingDetails card={{ ...worked, state: 'done' }} />);
    expect(screen.getByTestId('timing-state')).toHaveTextContent(`Shipped ${at(-60)}`);
    expect(screen.getByTestId('timing-holder')).toBeInTheDocument();
    expect(screen.getByTestId('timing-heartbeat')).toBeInTheDocument();
  });
});
