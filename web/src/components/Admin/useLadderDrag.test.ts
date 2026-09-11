import { describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';
import type { SelectorLadders } from '../../types';
import { useLadderDrag } from './useLadderDrag';

const DEFAULTS = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function ladders(): SelectorLadders {
  return { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } };
}

// The rail is 650px tall so a client y of (1 - v) * 1000 lands on prior v.
function rail(): { current: HTMLElement } {
  const el = document.createElement('div');
  el.getBoundingClientRect = () => ({ top: 0, height: 650, left: 0, width: 300, bottom: 650, right: 300, x: 0, y: 0, toJSON: () => ({}) });
  return { current: el };
}

function ev(clientY: number): ReactPointerEvent<HTMLElement> {
  return {
    clientY,
    pointerId: 1,
    button: 0,
    pointerType: 'mouse',
    preventDefault: vi.fn(),
    currentTarget: { setPointerCapture: vi.fn(), releasePointerCapture: vi.fn() },
  } as unknown as ReactPointerEvent<HTMLElement>;
}

function key(k: string): ReactKeyboardEvent<HTMLElement> {
  return { key: k, preventDefault: vi.fn() } as unknown as ReactKeyboardEvent<HTMLElement>;
}

function setup(linked: boolean, initial = ladders(), floor = 0.65) {
  const onChange = vi.fn();
  const railRef = rail();
  const hook = renderHook(
    ({ current }: { current: SelectorLadders }) => useLadderDrag({ ladders: current, linked, floor, railRef, onChange }),
    { initialProps: { current: initial } },
  );
  return { onChange, ...hook };
}

describe('useLadderDrag', () => {
  it('moves one ladder in 0.005 steps when unlinked', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onPointerDown(ev(180)));
    expect(result.current.dragging).toEqual({ role: 'coder', tier: 'complex' });

    act(() => props.onPointerMove(ev(145)));
    expect(onChange).toHaveBeenCalledTimes(1);
    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.855, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.82, 9);

    act(() => props.onPointerUp(ev(145)));
    expect(result.current.dragging).toBeNull();
  });

  it('writes the same value into both ladders when linked', () => {
    const { result, onChange } = setup(true);
    const props = result.current.handleProps('reviewer', 'complex');

    act(() => props.onPointerDown(ev(180)));
    act(() => props.onPointerMove(ev(145)));

    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.855, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.855, 9);
  });

  it('clamps between the neighbours of every ladder it writes to', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onPointerDown(ev(180)));
    act(() => props.onPointerMove(ev(50)));
    expect((onChange.mock.calls[0][0] as SelectorLadders).coder.complex).toBeCloseTo(0.9, 9);

    act(() => props.onPointerMove(ev(300)));
    expect((onChange.mock.calls[1][0] as SelectorLadders).coder.complex).toBeCloseTo(0.76, 9);
  });

  it('never drops the lowest bar below the catalog floor', () => {
    const raised = ladders();
    raised.coder.simple = 0.7;
    const { result, onChange, rerender } = setup(false, raised, 0.65);
    let props = result.current.handleProps('coder', 'simple');

    act(() => props.onPointerDown(ev(300)));
    act(() => props.onPointerMove(ev(500)));
    expect((onChange.mock.calls[0][0] as SelectorLadders).coder.simple).toBeCloseTo(0.65, 9);

    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.simple).toBeCloseTo(0.65, 9);

    // Already at the floor: a further pull below it changes nothing.
    rerender({ current: next });
    props = result.current.handleProps('coder', 'simple');
    act(() => props.onPointerMove(ev(600)));
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('does not report a change when the value is already in place', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onPointerDown(ev(180)));
    act(() => props.onPointerMove(ev(180)));
    expect(onChange).not.toHaveBeenCalled();
  });

  it('holds still on a linked drag when the two ladders leave no shared value', () => {
    const disjoint: SelectorLadders = {
      coder: { simple: 0.65, moderate: 0.85, complex: 0.9, critical: 0.95 },
      reviewer: { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.83 },
    };
    const { result, onChange } = setup(true, disjoint);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onPointerDown(ev(100)));
    act(() => props.onPointerMove(ev(140)));
    expect(onChange).not.toHaveBeenCalled();
  });

  it('steps one ladder by 0.005 per arrow key when unlinked', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    const up = key('ArrowUp');
    act(() => props.onKeyDown(up));
    expect(up.preventDefault).toHaveBeenCalled();
    let next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.825, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.82, 9);

    act(() => props.onKeyDown(key('ArrowDown')));
    next = onChange.mock.calls[1][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.815, 9);

    act(() => props.onKeyDown(key('ArrowRight')));
    expect((onChange.mock.calls[2][0] as SelectorLadders).coder.complex).toBeCloseTo(0.825, 9);

    act(() => props.onKeyDown(key('ArrowLeft')));
    expect((onChange.mock.calls[3][0] as SelectorLadders).coder.complex).toBeCloseTo(0.815, 9);
  });

  it('writes an arrow step into both ladders when linked', () => {
    const { result, onChange } = setup(true);
    const props = result.current.handleProps('reviewer', 'complex');

    act(() => props.onKeyDown(key('ArrowUp')));

    const next = onChange.mock.calls[0][0] as SelectorLadders;
    expect(next.coder.complex).toBeCloseTo(0.825, 9);
    expect(next.reviewer.complex).toBeCloseTo(0.825, 9);
  });

  it('stops an arrow step at the neighbouring bar', () => {
    const tight = ladders();
    tight.coder.complex = 0.898;
    const { result, onChange } = setup(false, tight);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onKeyDown(key('ArrowUp')));
    expect((onChange.mock.calls[0][0] as SelectorLadders).coder.complex).toBeCloseTo(0.9, 9);
  });

  it('Home and End move the bar to the ends of its own range', () => {
    const { result, onChange, rerender } = setup(false);
    let props = result.current.handleProps('coder', 'complex');

    act(() => props.onKeyDown(key('End')));
    const top = onChange.mock.calls[0][0] as SelectorLadders;
    expect(top.coder.complex).toBeCloseTo(0.9, 9);

    rerender({ current: top });
    props = result.current.handleProps('coder', 'complex');
    act(() => props.onKeyDown(key('Home')));
    expect((onChange.mock.calls[1][0] as SelectorLadders).coder.complex).toBeCloseTo(0.76, 9);
  });

  it('leaves keys it does not handle to the browser', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    const tab = key('Tab');
    act(() => props.onKeyDown(tab));

    expect(tab.preventDefault).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('holds still on a linked arrow step when the two ladders leave no shared value', () => {
    const disjoint: SelectorLadders = {
      coder: { simple: 0.65, moderate: 0.85, complex: 0.9, critical: 0.95 },
      reviewer: { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.83 },
    };
    const { result, onChange } = setup(true, disjoint);

    act(() => result.current.handleProps('coder', 'complex').onKeyDown(key('ArrowDown')));

    expect(onChange).not.toHaveBeenCalled();
  });

  it('ignores moves without a pointer down and clears on cancel', () => {
    const { result, onChange } = setup(false);
    const props = result.current.handleProps('coder', 'complex');

    act(() => props.onPointerMove(ev(145)));
    expect(onChange).not.toHaveBeenCalled();

    act(() => props.onPointerDown(ev(180)));
    act(() => props.onPointerCancel(ev(180)));
    expect(result.current.dragging).toBeNull();
  });
});
