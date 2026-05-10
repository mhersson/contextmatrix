import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { RefreshPlanModal } from './RefreshPlanModal';
import type { RefreshPlan } from '../../types';

const samplePlan: RefreshPlan = {
  head_commit: 'abc',
  items: [
    { doc: 'architecture.md', reason: 'missing', human_edited: false, estimated_cost_usd: 0.5 },
    { doc: 'api-documentation.md', reason: 'scheduled rebuild', human_edited: true, estimated_cost_usd: 0.3 },
  ],
};

const emptyPlan: RefreshPlan = {
  head_commit: 'abc',
  items: [],
};

describe('RefreshPlanModal', () => {
  it('renders non-edited rows as static "auto" labels and leaves human-edited rows as unchecked checkboxes', () => {
    render(
      <RefreshPlanModal
        plan={samplePlan}
        repo="core"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    // Only the human-edited row exposes an interactive checkbox.
    const checkboxes = screen.getAllByRole('checkbox');
    expect(checkboxes).toHaveLength(1);
    const api = screen.getByRole('checkbox', { name: /api-documentation\.md/ });
    expect(api).not.toBeChecked();
    // Non-edited row shows the static "auto" label.
    expect(screen.getByText('auto')).toBeInTheDocument();
    expect(
      screen.getByLabelText(/architecture\.md rebuilds automatically/),
    ).toBeInTheDocument();
  });

  it('emits the user-approved overwrite list (human-edited docs only)', () => {
    const onConfirm = vi.fn();
    render(
      <RefreshPlanModal
        plan={samplePlan}
        repo="core"
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole('checkbox', { name: /api-documentation\.md/ }));
    fireEvent.click(screen.getByRole('button', { name: /refresh/i }));

    expect(onConfirm).toHaveBeenCalledWith(['api-documentation.md']);
  });

  it('does not surface estimated cost amounts', () => {
    render(
      <RefreshPlanModal plan={samplePlan} repo="core" onConfirm={() => {}} onCancel={() => {}} />,
    );
    expect(screen.queryByText(/\$\d/)).not.toBeInTheDocument();
  });

  it('cancel button calls onCancel', () => {
    const onCancel = vi.fn();
    render(
      <RefreshPlanModal plan={samplePlan} repo="core" onConfirm={() => {}} onCancel={onCancel} />,
    );
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onCancel).toHaveBeenCalled();
  });

  it('disables Confirm while onConfirm is in flight', async () => {
    let resolve: () => void = () => {};
    const onConfirm = vi.fn(
      () =>
        new Promise<void>((r) => {
          resolve = r;
        }),
    );
    render(
      <RefreshPlanModal
        plan={samplePlan}
        repo="core"
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );
    const confirm = screen.getByRole('button', { name: /refresh/i });
    fireEvent.click(confirm);
    await waitFor(() => expect(confirm).toBeDisabled());
    expect(onConfirm).toHaveBeenCalledTimes(1);
    // Second click while submitting should be a no-op.
    fireEvent.click(confirm);
    expect(onConfirm).toHaveBeenCalledTimes(1);
    resolve();
    await waitFor(() => expect(confirm).not.toBeDisabled());
  });

  it('disables Confirm when plan has no items', () => {
    render(
      <RefreshPlanModal plan={emptyPlan} repo="core" onConfirm={() => {}} onCancel={() => {}} />,
    );
    const confirm = screen.getByRole('button', { name: /refresh/i });
    expect(confirm).toBeDisabled();
  });
});
