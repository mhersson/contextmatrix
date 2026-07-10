import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ChatSection } from './ChatSection';
import { MobileSidebarProvider } from '../../context/MobileSidebarContext';
import { api } from '../../api/client';
import type { ChatSession } from '../../types';

// Mock localStorage with a real backing store + spy-able methods so tests
// can assert on reads/writes without depending on the jsdom implementation.
const localStorageMock = (() => {
  let store: Record<string, string> = {};
  return {
    getItem: vi.fn((key: string) => store[key] ?? null),
    setItem: vi.fn((key: string, value: string) => {
      store[key] = value;
    }),
    removeItem: vi.fn((key: string) => {
      delete store[key];
    }),
    clear: vi.fn(() => {
      store = {};
    }),
  };
})();

Object.defineProperty(globalThis, 'localStorage', { value: localStorageMock, configurable: true });

const fixtures: ChatSession[] = [
  { id: 'S1', title: 'worker-auth', status: 'active', created_at: '', last_active: '', created_by: 'x' },
  { id: 'S2', title: 'triage', status: 'cold', created_at: '', last_active: '', created_by: 'x' },
];

describe('ChatSection', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listChats').mockResolvedValue(fixtures);
    localStorageMock.clear();
    localStorageMock.getItem.mockClear();
    localStorageMock.setItem.mockClear();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders sessions with status dots', async () => {
    render(
      <MemoryRouter>
        <MobileSidebarProvider><ChatSection onNewChat={() => {}} /></MobileSidebarProvider>
      </MemoryRouter>,
    );
    expect(await screen.findByText('worker-auth')).toBeInTheDocument();
    expect(screen.getByText('triage')).toBeInTheDocument();
  });

  it('persists collapse state in localStorage', async () => {
    render(
      <MemoryRouter>
        <MobileSidebarProvider><ChatSection onNewChat={() => {}} /></MobileSidebarProvider>
      </MemoryRouter>,
    );
    await screen.findByText('worker-auth');
    fireEvent.click(screen.getByRole('button', { name: /Chat/ }));
    expect(localStorageMock.setItem).toHaveBeenCalledWith('sidebar.chat_section_collapsed', '1');
  });

  it('calls onNewChat when "+ new" is clicked', async () => {
    const onNewChat = vi.fn();
    render(
      <MemoryRouter>
        <MobileSidebarProvider><ChatSection onNewChat={onNewChat} /></MobileSidebarProvider>
      </MemoryRouter>,
    );
    await screen.findByText('worker-auth');
    fireEvent.click(screen.getByTitle('New chat'));
    expect(onNewChat).toHaveBeenCalledOnce();
  });
});
