import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CardSignalIcons } from './CardSignalIcons';
import type { Card } from '../../types';

vi.mock('../../hooks/useSentinel', () => ({
  useSentinel: () => ({ info: { status: { enabled: true } } }),
}));

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Signal card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

describe('CardSignalIcons Sentinel shield', () => {
  it('animates the shield only while the mark is working', () => {
    const label = 'Sentinel is analysing the last run';
    const { rerender } = render(
      <CardSignalIcons card={{ ...baseCard, sentinel: { action: 'analysing', label, working: true } }} />,
    );
    expect(screen.getByRole('img', { name: label })).toHaveClass('sentinel-working');

    rerender(<CardSignalIcons card={{ ...baseCard, sentinel: { action: 'analysing', label } }} />);
    expect(screen.getByRole('img', { name: label })).not.toHaveClass('sentinel-working');

    const healed = 'Sentinel healed this card';
    rerender(<CardSignalIcons card={{ ...baseCard, sentinel: { action: 'healed', label: healed, working: false } }} />);
    expect(screen.getByRole('img', { name: healed })).not.toHaveClass('sentinel-working');
  });
});
