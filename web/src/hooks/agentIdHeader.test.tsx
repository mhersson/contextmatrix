import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { api } from '../api/client';
import { AuthGate } from '../components/Auth';
import { AuthProvider } from './useAuth';
import { AttentionProvider } from './useAttention';

vi.mock('./useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, reconnectEpoch: 0 }),
}));

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } });

function serve(authMode: 'none' | 'multi') {
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) => {
    const path = String(input);
    if (path === '/api/app/config') return json({ theme: 'everforest', version: 'x', auth_mode: authMode });
    if (path === '/api/auth/session') return json({ username: 'alice', display_name: '', is_admin: false });
    return json([]);
  });
}

async function firstAttentionHeaders(fetchSpy: ReturnType<typeof serve>): Promise<Headers> {
  render(
    <MemoryRouter>
      <AuthProvider>
        <AuthGate>
          <AttentionProvider>{null}</AttentionProvider>
        </AuthGate>
      </AuthProvider>
    </MemoryRouter>,
  );
  await waitFor(() => expect(fetchSpy.mock.calls.some(([p]) => p === '/api/attention')).toBe(true));
  const [, init] = fetchSpy.mock.calls.find(([p]) => p === '/api/attention')!;
  return new Headers(init?.headers);
}

afterEach(() => {
  vi.restoreAllMocks();
  api.setAgentId(null);
  localStorage.clear();
});

describe('the browser agent ID on app start', () => {
  it('rides on the first attention fetch in none mode', async () => {
    const headers = await firstAttentionHeaders(serve('none'));
    expect(headers.get('X-Agent-ID')).toMatch(/^human:web-/);
    expect(headers.get('X-Agent-ID')).toBe(localStorage.getItem('contextmatrix-agent-id'));
  });

  it('stays off in multi mode, where the session names the viewer', async () => {
    const headers = await firstAttentionHeaders(serve('multi'));
    expect(headers.get('X-Agent-ID')).toBeNull();
    expect(localStorage.getItem('contextmatrix-agent-id')).toBeNull();
  });
});
