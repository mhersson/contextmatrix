import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useChatLiveData, setChatLiveData, clearChatLiveData, subscribeChatLiveData } from './useChatLiveData';

const CHAT_ID = 'test-chat-001';

beforeEach(() => {
  // Clean store state between tests so they don't bleed into each other.
  clearChatLiveData(CHAT_ID);
});

describe('useChatLiveData — cost fields', () => {
  it('stores and reads cost fields', () => {
    const { result } = renderHook(() => useChatLiveData(CHAT_ID));

    act(() => {
      setChatLiveData(CHAT_ID, {
        estimatedCostUsd: 0.05,
        promptTokens: 100,
        completionTokens: 50,
        cacheReadTokens: 25,
        cacheCreationTokens: 10,
      });
    });

    expect(result.current?.estimatedCostUsd).toBe(0.05);
    expect(result.current?.promptTokens).toBe(100);
    expect(result.current?.completionTokens).toBe(50);
    expect(result.current?.cacheReadTokens).toBe(25);
    expect(result.current?.cacheCreationTokens).toBe(10);
  });

  it('no-op writes are skipped — identical cost values trigger only one notification', () => {
    const subscriber = vi.fn();
    const unsubscribe = subscribeChatLiveData(subscriber);

    const costPayload = {
      estimatedCostUsd: 0.05,
      promptTokens: 100,
      completionTokens: 50,
      cacheReadTokens: 25,
      cacheCreationTokens: 10,
    };

    try {
      // First write — changes state, must notify.
      setChatLiveData(CHAT_ID, costPayload);
      expect(subscriber).toHaveBeenCalledTimes(1);

      // Second write with identical values — shallowEqualLive must suppress notify.
      setChatLiveData(CHAT_ID, costPayload);
      expect(subscriber).toHaveBeenCalledTimes(1);
    } finally {
      unsubscribe();
    }
  });
});
