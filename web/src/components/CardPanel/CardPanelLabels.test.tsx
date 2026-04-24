import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { LabelsSection } from './CardPanelLabels';

describe('LabelsSection — rendering', () => {
  it('renders each label as a chip', () => {
    render(
      <LabelsSection
        editedLabels={['bug', 'p1']}
        disabled={false}
        onLabelsChange={vi.fn()}
      />,
    );
    expect(screen.getByText('bug')).toBeInTheDocument();
    expect(screen.getByText('p1')).toBeInTheDocument();
  });

  it('renders the "+ add" button when enabled and not currently adding', () => {
    render(
      <LabelsSection editedLabels={[]} disabled={false} onLabelsChange={vi.fn()} />,
    );
    expect(screen.getByRole('button', { name: /\+ add/ })).toBeInTheDocument();
  });
});

describe('LabelsSection — add flow', () => {
  it('adds a trimmed label on Enter', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection
        editedLabels={['bug']}
        disabled={false}
        onLabelsChange={onLabelsChange}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /\+ add/ }));
    const input = screen.getByRole('textbox', { name: 'Add label' });
    fireEvent.change(input, { target: { value: '  new-label  ' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onLabelsChange).toHaveBeenCalledWith(['bug', 'new-label']);
  });

  it('adds a label via the Add button', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection
        editedLabels={[]}
        disabled={false}
        onLabelsChange={onLabelsChange}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /\+ add/ }));
    const input = screen.getByRole('textbox', { name: 'Add label' });
    fireEvent.change(input, { target: { value: 'shiny' } });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(onLabelsChange).toHaveBeenCalledWith(['shiny']);
  });

  it('dedups (case-sensitive) and does not fire onLabelsChange', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection
        editedLabels={['bug']}
        disabled={false}
        onLabelsChange={onLabelsChange}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /\+ add/ }));
    const input = screen.getByRole('textbox', { name: 'Add label' });
    fireEvent.change(input, { target: { value: 'bug' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onLabelsChange).not.toHaveBeenCalled();
  });

  it('ignores empty / whitespace-only input', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection editedLabels={[]} disabled={false} onLabelsChange={onLabelsChange} />,
    );
    fireEvent.click(screen.getByRole('button', { name: /\+ add/ }));
    const input = screen.getByRole('textbox', { name: 'Add label' });
    fireEvent.change(input, { target: { value: '   ' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onLabelsChange).not.toHaveBeenCalled();
  });

  it('cancels add via Escape', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection editedLabels={[]} disabled={false} onLabelsChange={onLabelsChange} />,
    );
    fireEvent.click(screen.getByRole('button', { name: /\+ add/ }));
    const input = screen.getByRole('textbox', { name: 'Add label' });
    fireEvent.change(input, { target: { value: 'typed' } });
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(onLabelsChange).not.toHaveBeenCalled();
    // The input is unmounted again after Escape → + add button is back.
    expect(screen.getByRole('button', { name: /\+ add/ })).toBeInTheDocument();
  });
});

describe('LabelsSection — remove flow', () => {
  it('removes a label via its × button', () => {
    const onLabelsChange = vi.fn();
    render(
      <LabelsSection
        editedLabels={['bug', 'p1']}
        disabled={false}
        onLabelsChange={onLabelsChange}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Remove label bug' }));
    expect(onLabelsChange).toHaveBeenCalledWith(['p1']);
  });
});

describe('LabelsSection — disabled mode', () => {
  it('hides the add button and × removers when disabled', () => {
    render(
      <LabelsSection
        editedLabels={['bug']}
        disabled
        onLabelsChange={vi.fn()}
      />,
    );
    expect(screen.queryByRole('button', { name: /\+ add/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Remove label/ })).not.toBeInTheDocument();
  });

  it('renders the locked-reason hint with default copy when none is provided', () => {
    render(
      <LabelsSection editedLabels={[]} disabled onLabelsChange={vi.fn()} />,
    );
    expect(screen.getByText(/locked while agent owns this card/)).toBeInTheDocument();
  });

  it('uses the caller-supplied locked reason verbatim', () => {
    render(
      <LabelsSection
        editedLabels={['bug']}
        disabled
        lockedReason="custom lock message"
        onLabelsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/custom lock message/)).toBeInTheDocument();
  });
});
