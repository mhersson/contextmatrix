import { beforeEach, describe, it, expect } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { flippedSettings, mockupChatExtra, publishWaitingMockup, resetMockupStore, useMockupSettings } from './mockupStore';

const opening = { layout: 'rows', group: 'off' };

beforeEach(() => resetMockupStore());

describe('useMockupSettings', () => {
  it('opens at the opening settings with nothing flipped', () => {
    const { result } = renderHook(() => useMockupSettings('S1', 1, opening, false));
    expect(result.current[0]).toBe(opening);
    expect(flippedSettings('S1', 1)).toBeNull();
  });

  it('a flip updates the value and keeps the full set', () => {
    const { result } = renderHook(() => useMockupSettings('S1', 1, opening, false));
    act(() => result.current[1]('group', 'on'));
    expect(result.current[0]).toEqual({ layout: 'rows', group: 'on' });
    expect(flippedSettings('S1', 1)).toEqual({ layout: 'rows', group: 'on' });
  });

  it('keeps revisions and chats apart', () => {
    const one = renderHook(() => useMockupSettings('S1', 1, opening, false));
    const two = renderHook(() => useMockupSettings('S1', 2, opening, false));
    const other = renderHook(() => useMockupSettings('S2', 1, opening, false));
    act(() => one.result.current[1]('layout', 'cards'));
    expect(one.result.current[0].layout).toBe('cards');
    expect(two.result.current[0]).toBe(opening);
    expect(other.result.current[0]).toBe(opening);
    expect(flippedSettings('S1', 2)).toBeNull();
  });

  it('returns the opening settings and ignores flips when fixed', () => {
    const { result } = renderHook(() => useMockupSettings('S1', 1, opening, true));
    act(() => result.current[1]('group', 'on'));
    expect(result.current[0]).toBe(opening);
    expect(flippedSettings('S1', 1)).toBeNull();
  });

  it('a fixed revision shows opening even when it was flipped before', () => {
    const live = renderHook(() => useMockupSettings('S1', 1, opening, false));
    act(() => live.result.current[1]('group', 'on'));
    const fixed = renderHook(() => useMockupSettings('S1', 1, opening, true));
    expect(fixed.result.current[0]).toBe(opening);
  });
});

describe('mockupChatExtra', () => {
  const full = { layout: 'cards', group: 'on' };

  it('a chat names the waiting revision and carries your flips of it', () => {
    publishWaitingMockup('S1', { promptId: 'mockup:1', revision: 2 });
    const { result } = renderHook(() => useMockupSettings('S1', 2, { layout: 'rows', group: 'off' }, false));
    act(() => result.current[1]('layout', 'cards'));
    act(() => result.current[1]('group', 'on'));
    expect(mockupChatExtra('S1')).toEqual({
      mockup: { prompt_id: 'mockup:1', revision: 2, action: 'chat', settings: full },
    });
  });

  it('carries no settings when you have not flipped that revision', () => {
    publishWaitingMockup('S1', { promptId: 'mockup:1', revision: 2 });
    expect(mockupChatExtra('S1')).toEqual({ mockup: { prompt_id: 'mockup:1', revision: 2, action: 'chat' } });
    const one = renderHook(() => useMockupSettings('S1', 1, opening, false));
    act(() => one.result.current[1]('group', 'on'));
    expect(mockupChatExtra('S1')?.mockup).not.toHaveProperty('settings');
  });

  it('is plain when no mockup waits', () => {
    publishWaitingMockup('S1', { promptId: 'mockup:1', revision: 2 });
    publishWaitingMockup('S1', null);
    expect(mockupChatExtra('S1')).toBeUndefined();
    expect(mockupChatExtra('S2')).toBeUndefined();
  });
});
