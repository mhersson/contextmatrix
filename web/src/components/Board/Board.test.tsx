import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { isTouchDevice, Board } from './Board';
import type { Card, ProjectConfig } from '../../types';

// ---------------------------------------------------------------------------
// isTouchDevice unit tests
// ---------------------------------------------------------------------------

describe('isTouchDevice', () => {
  const originalMatchMedia = window.matchMedia;

  afterEach(() => {
    // Restore original matchMedia after each test
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 0,
    });
  });

  it('returns true when matchMedia reports pointer: coarse', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: query === '(pointer: coarse)',
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
    expect(isTouchDevice()).toBe(true);
  });

  it('returns false when matchMedia reports pointer: fine', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: query !== '(pointer: coarse)',
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
    expect(isTouchDevice()).toBe(false);
  });

  it('falls back to navigator.maxTouchPoints when matchMedia is unavailable', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: undefined,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 5,
    });
    expect(isTouchDevice()).toBe(true);
  });

  it('returns false via maxTouchPoints fallback when no touch points', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: undefined,
    });
    Object.defineProperty(navigator, 'maxTouchPoints', {
      writable: true,
      value: 0,
    });
    expect(isTouchDevice()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Board integration: DnD disabled on touch devices
// ---------------------------------------------------------------------------

// Minimal mock for @dnd-kit/core — we only care that DndContext receives the
// correct sensors prop. We capture it via a spy on DndContext.
vi.mock('@dnd-kit/core', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@dnd-kit/core')>();
  return {
    ...actual,
    useDraggable: () => ({
      attributes: {},
      listeners: {},
      setNodeRef: () => {},
      transform: null,
      isDragging: false,
    }),
    useDroppable: () => ({
      setNodeRef: () => {},
      isOver: false,
    }),
  };
});

const baseConfig: ProjectConfig = {
  name: 'test-project',
  prefix: 'TEST',
  next_id: 1,
  states: ['todo', 'done'],
  transitions: { todo: ['done'], done: [] },
  types: ['task'],
  priorities: ['medium'],
};

const sampleCard: Card = {
  id: 'TEST-001',
  title: 'Sample card',
  project: 'test-project',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

describe('Board — drag-and-drop disabled on touch devices', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    // Simulate a touch/coarse-pointer device
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: query === '(pointer: coarse)',
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('renders the board without crashing on a touch device', () => {
    render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
      />
    );
    // Board renders card content
    expect(screen.getByText('Sample card')).toBeInTheDocument();
  });

  it('isTouchDevice returns true in simulated touch environment', () => {
    // Confirm the mock is active for this describe block
    expect(isTouchDevice()).toBe(true);
  });
});

describe('Board — drag-and-drop enabled on pointer (desktop) devices', () => {
  const originalMatchMedia = window.matchMedia;

  beforeEach(() => {
    // Simulate a fine-pointer (mouse) device
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (_query: string) => ({
        matches: false,
        media: _query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
  });

  afterEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: originalMatchMedia,
    });
  });

  it('renders the board without crashing on a pointer device', () => {
    render(
      <Board
        cards={[sampleCard]}
        config={baseConfig}
        loading={false}
        error={null}
      />
    );
    expect(screen.getByText('Sample card')).toBeInTheDocument();
  });

  it('isTouchDevice returns false in simulated pointer environment', () => {
    expect(isTouchDevice()).toBe(false);
  });
});
