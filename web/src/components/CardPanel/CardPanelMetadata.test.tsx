import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { CardPanelMetadata } from './CardPanelMetadata';
import type { Card } from '../../types';

// Mock api.getCard to prevent real network requests in tests
vi.mock('../../api/client', () => ({
  api: {
    getCard: vi.fn().mockResolvedValue({ state: 'todo' }),
  },
}));

const baseCard: Card = {
  id: 'TEST-001',
  title: 'Test card',
  project: 'test',
  type: 'task',
  state: 'todo',
  priority: 'medium',
  created: '2026-01-01T00:00:00Z',
  updated: '2026-01-01T00:00:00Z',
  body: '',
};

const subtaskCard: Card = {
  ...baseCard,
  id: 'TEST-002',
  type: 'subtask',
  parent: 'TEST-001',
};

const automationProps = {
  editedAutonomous: false,
  editedUseOpusOrchestrator: false,
  editedFeatureBranch: false,
  editedCreatePR: false,
  onAutonomousChange: vi.fn(),
  onUseOpusOrchestratorChange: vi.fn(),
  onFeatureBranchChange: vi.fn(),
  onCreatePRChange: vi.fn(),
  editedVetted: false,
  onVettedChange: vi.fn(),
  branches: [],
  onBaseBranchChange: vi.fn(),
};

describe('CardPanelMetadata — parent section', () => {
  it('renders Parent section when card.parent is defined', () => {
    render(
      <CardPanelMetadata
        card={subtaskCard}
        editedLabels={[]}
        onLabelsChange={vi.fn()}
        onSubtaskClick={vi.fn()}
        {...automationProps}
      />,
    );
    expect(screen.getByText('Parent')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'TEST-001' })).toBeInTheDocument();
  });

  it('does not render Parent section when card.parent is absent', () => {
    render(
      <CardPanelMetadata
        card={baseCard}
        editedLabels={[]}
        onLabelsChange={vi.fn()}
        onSubtaskClick={vi.fn()}
        {...automationProps}
      />,
    );
    expect(screen.queryByText('Parent')).not.toBeInTheDocument();
  });

  it('calls onSubtaskClick with the parent card ID when the parent button is clicked', () => {
    const onSubtaskClick = vi.fn();
    render(
      <CardPanelMetadata
        card={subtaskCard}
        editedLabels={[]}
        onLabelsChange={vi.fn()}
        onSubtaskClick={onSubtaskClick}
        {...automationProps}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'TEST-001' }));
    expect(onSubtaskClick).toHaveBeenCalledOnce();
    expect(onSubtaskClick).toHaveBeenCalledWith('TEST-001');
  });

  it('Parent section appears above Subtasks section when both are present', () => {
    const cardWithBoth: Card = {
      ...subtaskCard,
      subtasks: ['TEST-003'],
    };
    render(
      <CardPanelMetadata
        card={cardWithBoth}
        editedLabels={[]}
        onLabelsChange={vi.fn()}
        onSubtaskClick={vi.fn()}
        {...automationProps}
      />,
    );

    const labels = screen.getAllByText(/Parent|Subtasks/);
    expect(labels[0]).toHaveTextContent('Parent');
    expect(labels[1]).toHaveTextContent('Subtasks');
  });
});
