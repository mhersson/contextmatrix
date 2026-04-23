import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ConfirmModal } from './ConfirmModal';

const defaultProps = {
  open: true,
  title: 'Are you sure?',
  message: 'This action cannot be undone.',
  onConfirm: vi.fn(),
  onCancel: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
});

describe('ConfirmModal — rendering', () => {
  it('renders title, message, and both button labels when open=true', () => {
    render(<ConfirmModal {...defaultProps} />);
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Are you sure?')).toBeInTheDocument();
    expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeInTheDocument();
  });

  it('renders nothing when open=false', () => {
    const { container } = render(<ConfirmModal {...defaultProps} open={false} />);
    expect(container.firstChild).toBeNull();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('uses custom confirmLabel and cancelLabel when provided', () => {
    render(<ConfirmModal {...defaultProps} confirmLabel="Delete" cancelLabel="Go back" />);
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Go back' })).toBeInTheDocument();
  });

  it('renders ReactNode message', () => {
    render(
      <ConfirmModal
        {...defaultProps}
        message={<span data-testid="node-msg">Custom node</span>}
      />
    );
    expect(screen.getByTestId('node-msg')).toBeInTheDocument();
  });
});

describe('ConfirmModal — callbacks', () => {
  it('clicking Confirm calls onConfirm', () => {
    render(<ConfirmModal {...defaultProps} />);
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(defaultProps.onConfirm).toHaveBeenCalledOnce();
    expect(defaultProps.onCancel).not.toHaveBeenCalled();
  });

  it('clicking Cancel calls onCancel', () => {
    render(<ConfirmModal {...defaultProps} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(defaultProps.onCancel).toHaveBeenCalledOnce();
    expect(defaultProps.onConfirm).not.toHaveBeenCalled();
  });
});

describe('ConfirmModal — keyboard', () => {
  it('pressing Escape calls onCancel', () => {
    render(<ConfirmModal {...defaultProps} />);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(defaultProps.onCancel).toHaveBeenCalledOnce();
  });
});

describe('ConfirmModal — backdrop', () => {
  it('clicking backdrop calls onCancel', () => {
    render(<ConfirmModal {...defaultProps} />);
    // The backdrop is aria-hidden div that wraps behind the dialog
    const backdrop = document.querySelector('[aria-hidden="true"]') as HTMLElement;
    expect(backdrop).not.toBeNull();
    fireEvent.click(backdrop);
    expect(defaultProps.onCancel).toHaveBeenCalledOnce();
  });

  it('clicking inside the panel does not call onCancel', () => {
    render(<ConfirmModal {...defaultProps} />);
    fireEvent.click(screen.getByRole('dialog'));
    expect(defaultProps.onCancel).not.toHaveBeenCalled();
  });
});

describe('ConfirmModal — variant', () => {
  it('variant="danger" applies red accent to confirm button', () => {
    render(<ConfirmModal {...defaultProps} confirmLabel="Delete" variant="danger" />);
    const confirmBtn = screen.getByRole('button', { name: 'Delete' });
    // Check inline style contains var(--red)
    expect(confirmBtn.style.color).toBe('var(--red)');
    expect(confirmBtn.style.background).toBe('var(--bg-red)');
  });

  it('variant="default" applies aqua accent to confirm button', () => {
    render(<ConfirmModal {...defaultProps} variant="default" />);
    const confirmBtn = screen.getByRole('button', { name: 'Confirm' });
    expect(confirmBtn.style.color).toBe('var(--aqua)');
    expect(confirmBtn.style.background).toBe('var(--bg-blue)');
  });
});

describe('ConfirmModal — accessibility', () => {
  it('dialog has role="dialog" and aria-modal="true"', () => {
    render(<ConfirmModal {...defaultProps} />);
    const dialog = screen.getByRole('dialog');
    expect(dialog).toHaveAttribute('aria-modal', 'true');
  });

  it('dialog has aria-labelledby pointing to the title element', () => {
    render(<ConfirmModal {...defaultProps} />);
    const dialog = screen.getByRole('dialog');
    const labelledBy = dialog.getAttribute('aria-labelledby');
    expect(labelledBy).toBeTruthy();
    const titleEl = document.getElementById(labelledBy!);
    expect(titleEl).not.toBeNull();
    expect(titleEl!.textContent).toBe('Are you sure?');
  });

  it('dialog has aria-describedby pointing to the message element', () => {
    render(<ConfirmModal {...defaultProps} />);
    const dialog = screen.getByRole('dialog');
    const describedBy = dialog.getAttribute('aria-describedby');
    expect(describedBy).toBeTruthy();
    const msgEl = document.getElementById(describedBy!);
    expect(msgEl).not.toBeNull();
    expect(msgEl!.textContent).toBe('This action cannot be undone.');
  });

  it('initial focus lands on the Confirm button', () => {
    render(<ConfirmModal {...defaultProps} />);
    const confirmBtn = screen.getByRole('button', { name: 'Confirm' });
    expect(document.activeElement).toBe(confirmBtn);
  });
});
