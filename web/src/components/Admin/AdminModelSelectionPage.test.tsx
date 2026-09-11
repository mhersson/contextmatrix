import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import { AdminModelSelectionPage } from './AdminModelSelectionPage';
import type { ModelBlacklistEntry, SelectorCandidatesResponse, SelectorLadders, SelectorLaddersResponse, SelectorPreview } from '../../types';
import { CANDIDATES, previewFixture } from './selector.fixtures';

const mocks = vi.hoisted(() => ({
  adminSelectorLadders: vi.fn(),
  adminSelectorPutLadders: vi.fn(),
  adminSelectorCandidates: vi.fn(),
  adminSelectorPreview: vi.fn(),
  adminModelBlacklist: vi.fn(),
  adminDelistModel: vi.fn(),
  adminBlacklistModel: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      adminSelectorLadders: mocks.adminSelectorLadders,
      adminSelectorPutLadders: mocks.adminSelectorPutLadders,
      adminSelectorCandidates: mocks.adminSelectorCandidates,
      adminSelectorPreview: mocks.adminSelectorPreview,
      adminModelBlacklist: mocks.adminModelBlacklist,
      adminDelistModel: mocks.adminDelistModel,
      adminBlacklistModel: mocks.adminBlacklistModel,
    },
  };
});

const DEFAULTS = { simple: 0.65, moderate: 0.76, complex: 0.82, critical: 0.9 };

function laddersRes(ladders: SelectorLadders, is_default = false, headroom = 1.5): SelectorLaddersResponse {
  return {
    ladders,
    defaults: { ...DEFAULTS },
    headroom,
    headroom_default: 1.5,
    is_default,
    updated_at: is_default ? undefined : '2026-09-10T08:30:00Z',
  };
}

function savedLadders(): SelectorLadders {
  return { coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS, critical: 0.93 } };
}

function catalogRes(): SelectorCandidatesResponse {
  return {
    candidates: CANDIDATES,
    favorites: [],
    blacklist: ['c/weak'],
    quality_floor: 0.65,
    catalog_refreshed_at: new Date(Date.now() - 6 * 3600 * 1000).toISOString(),
    reasoning_effort: '',
  };
}

function blacklistEntry(overrides: Partial<ModelBlacklistEntry> = {}): ModelBlacklistEntry {
  return {
    slug: 'moonshotai/kimi-k3',
    reason: 'tool calls failed to parse on 3 consecutive turns',
    sample_card: 'CM-101',
    reported_by: 'agent:worker-1',
    first_seen: 1756400000,
    last_seen: 1756500000,
    ...overrides,
  };
}

// jsdom has neither pointer capture nor layout; the rail is 650px tall so a
// client y of (1 - v) * 1000 lands on prior v. Plain stubs rather than spies:
// the per-test vi.resetAllMocks() below would otherwise restore the real
// getBoundingClientRect and every drag would read a zero-height rail.
beforeAll(() => {
  Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', { value: () => {}, configurable: true });
  Object.defineProperty(HTMLElement.prototype, 'releasePointerCapture', { value: () => {}, configurable: true });
  Object.defineProperty(HTMLElement.prototype, 'getBoundingClientRect', {
    value: () =>
      ({
        top: 0, height: 650, left: 0, width: 300, bottom: 650, right: 300, x: 0, y: 0, toJSON: () => ({}),
      }) as DOMRect,
    configurable: true,
  });
});

beforeEach(() => {
  vi.resetAllMocks();
  mocks.adminSelectorLadders.mockResolvedValue(laddersRes(savedLadders()));
  mocks.adminSelectorCandidates.mockResolvedValue(catalogRes());
  mocks.adminSelectorPreview.mockResolvedValue(previewFixture());
  mocks.adminModelBlacklist.mockResolvedValue({ models: [] });
});

function drag(name: string, from: number, to: number) {
  const handle = screen.getByRole('slider', { name });
  fireEvent.pointerDown(handle, { pointerId: 1, clientY: from, button: 0 });
  fireEvent.pointerMove(handle, { pointerId: 1, clientY: to });
  fireEvent.pointerUp(handle, { pointerId: 1, clientY: to });
}

async function renderLoaded() {
  render(<AdminModelSelectionPage />);
  await waitFor(() => expect(screen.getByRole('slider', { name: 'coder complex bar' })).toBeInTheDocument());
  await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenCalled());
}

describe('AdminModelSelectionPage - loading', () => {
  it('renders the saved ladders, the catalog meta and the first preview', async () => {
    await renderLoaded();

    expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run');
    expect(screen.getByText(/4 candidates · priors normalised to the AA leader · refreshed 6 h ago/)).toBeInTheDocument();
    expect(screen.getByRole('slider', { name: 'reviewer critical bar' })).toHaveTextContent('0.93');
    expect(screen.getByRole('slider', { name: 'coder critical bar' })).toHaveTextContent('0.90 · 0.93');
    expect(screen.getByTestId('tl-dot-coder-c/weak')).toHaveClass('banned');
    await waitFor(() => expect(screen.getByTestId('tl-seat-complex-1')).toHaveTextContent('walked'));
    expect(screen.getByTestId('tl-dot-reviewer-a/cheap')).toHaveClass('picked');
    expect(screen.getByTestId('tl-dot-reviewer-b/pricey')).toHaveClass('seat');
    expect(screen.getByTestId('tl-kpi-reviewers')).toHaveTextContent('3');
    expect(mocks.adminSelectorPreview).toHaveBeenCalledWith(savedLadders(), 1.5, expect.any(AbortSignal));
  });

  it('shows the ladder empty state with the error when the catalog is unavailable', async () => {
    mocks.adminSelectorCandidates.mockRejectedValue({ code: 'CATALOG_UNAVAILABLE', error: 'catalog not available yet' });

    render(<AdminModelSelectionPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent('catalog not available yet');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run');
    expect(screen.queryByRole('slider')).not.toBeInTheDocument();
    expect(screen.getByText('Preview needs the candidate catalog.')).toBeInTheDocument();
    expect(screen.queryByText('Waiting for the first preview…')).not.toBeInTheDocument();
    expect(mocks.adminSelectorPreview).not.toHaveBeenCalled();
  });

  it('claims nothing and edits nothing when the saved ladders fail to load', async () => {
    mocks.adminSelectorLadders.mockRejectedValue({ code: 'INTERNAL_ERROR', error: 'failed to read the selector ladders' });

    render(<AdminModelSelectionPage />);

    expect(await screen.findByRole('alert')).toHaveTextContent('failed to read the selector ladders');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('ladders unavailable');
    expect(screen.getByTestId('tl-status')).not.toHaveTextContent('saved');
    expect(screen.queryByRole('slider')).not.toBeInTheDocument();
    expect(screen.queryByTestId('tl-kpi-reviewers')).not.toBeInTheDocument();

    for (const name of ['Reset to defaults', 'Discard changes', 'Save']) {
      expect(screen.getByRole('button', { name })).toBeDisabled();
    }

    expect(mocks.adminSelectorPreview).not.toHaveBeenCalled();
    expect(mocks.adminSelectorPutLadders).not.toHaveBeenCalled();
  });

  it('loads unlinked when the saved coder and reviewer ladders differ', async () => {
    await renderLoaded();

    expect(screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' })).toHaveAttribute('aria-checked', 'false');

    drag('coder complex bar', 180, 145);

    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.855');
    expect(screen.getByRole('slider', { name: 'reviewer complex bar' })).toHaveTextContent('0.82');
  });

  it('loads linked when the saved ladders are equal', async () => {
    mocks.adminSelectorLadders.mockResolvedValue(laddersRes({ coder: { ...DEFAULTS }, reviewer: { ...DEFAULTS } }, true));
    await renderLoaded();

    expect(screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' })).toHaveAttribute('aria-checked', 'true');
  });

  it('names the gateway effort in the catalog meta when one is configured', async () => {
    mocks.adminSelectorCandidates.mockResolvedValue({ ...catalogRes(), reasoning_effort: 'medium' });
    await renderLoaded();

    expect(screen.getByText(/4 candidates · priors normalised to the AA leader · gateway effort medium · refreshed 6 h ago/)).toBeInTheDocument();
  });
});

describe('AdminModelSelectionPage - edit, save, discard, reset', () => {
  it('a drag marks the page dirty; Save PUTs the draft, refetches and clears it', async () => {
    mocks.adminSelectorPutLadders.mockImplementation(async (ladders: SelectorLadders) => laddersRes(ladders));
    mocks.adminSelectorLadders
      .mockResolvedValueOnce(laddersRes(savedLadders()))
      .mockResolvedValueOnce(laddersRes({ coder: { ...DEFAULTS, complex: 0.855 }, reviewer: { ...DEFAULTS, complex: 0.855, critical: 0.93 } }));
    await renderLoaded();

    fireEvent.click(screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' }));
    drag('coder complex bar', 180, 145);

    expect(screen.getByTestId('tl-status')).toHaveTextContent('unsaved changes · next run still uses the saved values');
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.855');
    expect(screen.getByRole('slider', { name: 'reviewer complex bar' })).toHaveTextContent('0.855');
    await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(2));

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(mocks.adminSelectorPutLadders).toHaveBeenCalledTimes(1));
    const sent = mocks.adminSelectorPutLadders.mock.calls[0][0] as SelectorLadders;
    expect(sent.coder.complex).toBeCloseTo(0.855, 9);
    expect(sent.reviewer.complex).toBeCloseTo(0.855, 9);
    expect(sent.reviewer.critical).toBeCloseTo(0.93, 9);
    await waitFor(() => expect(mocks.adminSelectorLadders).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run'));
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it('Discard restores the saved ladders without a request', async () => {
    await renderLoaded();

    drag('coder complex bar', 180, 145);
    expect(screen.getByRole('button', { name: 'Discard changes' })).toBeEnabled();

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));

    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.82');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run');
    expect(mocks.adminSelectorPutLadders).not.toHaveBeenCalled();
  });

  it('Reset loads the defaults from the response into both roles, unsaved', async () => {
    await renderLoaded();

    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }));

    expect(screen.getByRole('slider', { name: 'reviewer critical bar' })).toHaveTextContent('0.90');
    expect(screen.getByRole('slider', { name: 'coder critical bar' })).toHaveTextContent('0.90');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('unsaved changes');
    expect(mocks.adminSelectorPutLadders).not.toHaveBeenCalled();
  });

  it('turning linked on snaps nothing; the next linked drag equalises the tier', async () => {
    mocks.adminSelectorLadders.mockResolvedValue(laddersRes({ coder: { ...DEFAULTS, complex: 0.9 }, reviewer: { ...DEFAULTS } }));
    await renderLoaded();

    const toggle = screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' });
    expect(toggle).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.90 · 0.82');

    fireEvent.click(toggle);

    expect(toggle).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.90 · 0.82');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('saved');

    drag('coder complex bar', 100, 145);

    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.855');
    expect(screen.getByRole('slider', { name: 'reviewer complex bar' })).toHaveTextContent('0.855');
  });

  it('an operator toggle survives a save and refetch', async () => {
    mocks.adminSelectorPutLadders.mockImplementation(async (ladders: SelectorLadders) => laddersRes(ladders));
    mocks.adminSelectorLadders
      .mockResolvedValueOnce(laddersRes(savedLadders()))
      .mockResolvedValueOnce(laddersRes({ coder: { ...DEFAULTS, complex: 0.855 }, reviewer: { ...DEFAULTS, complex: 0.855, critical: 0.93 } }));
    await renderLoaded();

    const toggle = screen.getByRole('switch', { name: 'Link the coder and reviewer ladders' });
    fireEvent.click(toggle);
    drag('coder complex bar', 180, 145);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(mocks.adminSelectorLadders).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run'));
    expect(toggle).toHaveAttribute('aria-checked', 'true');
  });

  it('surfaces a save failure inline and keeps the draft', async () => {
    mocks.adminSelectorPutLadders.mockRejectedValue({ code: 'VALIDATION_ERROR', error: 'invalid selector ladders' });
    await renderLoaded();

    drag('coder complex bar', 180, 145);
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText('invalid selector ladders')).toBeInTheDocument();
    expect(screen.getByRole('slider', { name: 'coder complex bar' })).toHaveTextContent('0.855');
    expect(screen.getByTestId('tl-status')).toHaveTextContent('unsaved changes');
  });

  it('the headroom is part of the draft, the preview and the save', async () => {
    mocks.adminSelectorPutLadders.mockImplementation(async (ladders: SelectorLadders, headroom: number) => laddersRes(ladders, false, headroom));
    mocks.adminSelectorLadders.mockResolvedValueOnce(laddersRes(savedLadders())).mockResolvedValueOnce(laddersRes(savedLadders(), false, 2));
    await renderLoaded();

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Price headroom' }), { target: { value: '2' } });

    expect(screen.getByTestId('tl-status')).toHaveTextContent('unsaved changes');
    expect(screen.getByText('headroom 2× · favorites and blacklist applied')).toBeInTheDocument();
    await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenLastCalledWith(savedLadders(), 2, expect.any(AbortSignal)));

    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(mocks.adminSelectorPutLadders).toHaveBeenCalledWith(savedLadders(), 2));
    await waitFor(() => expect(screen.getByTestId('tl-status')).toHaveTextContent('saved · in effect for the next run'));
    expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveValue(2);
  });

  it('a headroom below 1 disables Save and sends no preview', async () => {
    await renderLoaded();
    const calls = mocks.adminSelectorPreview.mock.calls.length;

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Price headroom' }), { target: { value: '0.5' } });

    expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Discard changes' })).toBeEnabled();
    expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(calls);
  });

  it('Reset restores the default headroom with the default ladders', async () => {
    mocks.adminSelectorLadders.mockResolvedValue(laddersRes(savedLadders(), false, 2));
    await renderLoaded();

    fireEvent.click(screen.getByRole('button', { name: 'Reset to defaults' }));

    expect(screen.getByRole('spinbutton', { name: 'Price headroom' })).toHaveValue(1.5);
    expect(screen.getByTestId('tl-status')).toHaveTextContent('unsaved changes');
  });
});

describe('AdminModelSelectionPage - preview', () => {
  it('is busy until the answer lands and keeps the last good preview on an error', async () => {
    let resolveFirst: (p: SelectorPreview) => void = () => {};
    mocks.adminSelectorPreview
      .mockImplementationOnce(() => new Promise<SelectorPreview>((resolve) => { resolveFirst = resolve; }))
      .mockRejectedValueOnce({ code: 'INTERNAL_ERROR', error: 'preview exploded' });

    render(<AdminModelSelectionPage />);
    await waitFor(() => expect(screen.getByRole('slider', { name: 'coder complex bar' })).toBeInTheDocument());
    await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(1));

    expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByText('Waiting for the first preview…')).toBeInTheDocument();

    resolveFirst(previewFixture());
    await waitFor(() => expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'false'));
    expect(screen.getByTestId('tl-seat-complex-0')).toBeInTheDocument();

    drag('coder complex bar', 180, 145);
    expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'true');

    expect(await screen.findByRole('alert')).toHaveTextContent('preview exploded');
    expect(screen.getByTestId('tl-preview')).toHaveAttribute('aria-busy', 'false');
    expect(screen.getByTestId('tl-seat-complex-0')).toBeInTheDocument();
  });

  it('debounces drag steps into one preview request', async () => {
    await renderLoaded();

    const handle = screen.getByRole('slider', { name: 'coder complex bar' });
    fireEvent.pointerDown(handle, { pointerId: 1, clientY: 180, button: 0 });
    for (const y of [170, 160, 150, 145]) fireEvent.pointerMove(handle, { pointerId: 1, clientY: y });
    fireEvent.pointerUp(handle, { pointerId: 1, clientY: 145 });

    await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(2));
    await new Promise((r) => setTimeout(r, 250));
    expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(2);
  });
});

describe('AdminModelSelectionPage - blacklist', () => {
  it('renders a row per blacklisted model with slug, reason, sample card, and reporter', async () => {
    mocks.adminModelBlacklist.mockResolvedValue({
      models: [
        blacklistEntry(),
        blacklistEntry({ slug: 'x-ai/grok-5-mini', reason: 'no forward progress', sample_card: '', reported_by: 'agent:worker-2' }),
      ],
    });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    expect(screen.getByText('x-ai/grok-5-mini')).toBeInTheDocument();
    expect(screen.getByText('tool calls failed to parse on 3 consecutive turns')).toBeInTheDocument();
    expect(screen.getByText('CM-101')).toBeInTheDocument();
    expect(screen.getByText('agent:worker-1')).toBeInTheDocument();
  });

  it('shows an empty-state message when nothing is blacklisted', async () => {
    render(<AdminModelSelectionPage />);

    expect(await screen.findByText(/no models are blacklisted/i)).toBeInTheDocument();
  });

  it('delist opens a confirm dialog, then deletes the slug and refetches on confirm', async () => {
    mocks.adminModelBlacklist
      .mockResolvedValueOnce({ models: [blacklistEntry()] })
      .mockResolvedValueOnce({ models: [] });
    mocks.adminDelistModel.mockResolvedValue({ deleted: 'moonshotai/kimi-k3' });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /delist/i }));

    await waitFor(() => expect(mocks.adminDelistModel).toHaveBeenCalledWith('moonshotai/kimi-k3'));
    await waitFor(() => expect(mocks.adminModelBlacklist).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText('moonshotai/kimi-k3')).not.toBeInTheDocument());
    // The catalog carries the blacklist the pills and the preview read, so
    // it is refetched too.
    await waitFor(() => expect(mocks.adminSelectorCandidates).toHaveBeenCalledTimes(2));
  });

  it('cancelling the delist dialog does not delete', async () => {
    mocks.adminModelBlacklist.mockResolvedValue({ models: [blacklistEntry()] });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /cancel/i }));

    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();
  });

  it('surfaces a delist failure as an inline error without crashing', async () => {
    mocks.adminModelBlacklist.mockResolvedValue({ models: [blacklistEntry()] });
    mocks.adminDelistModel.mockRejectedValue({ code: 'INTERNAL_ERROR', error: 'failed to delete blacklist entry' });

    render(<AdminModelSelectionPage />);

    await waitFor(() => expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Delist moonshotai/kimi-k3' }));

    const dialog = await screen.findByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /delist/i }));

    await waitFor(() => expect(mocks.adminDelistModel).toHaveBeenCalledTimes(1));
    expect(await screen.findByText(/failed to delete blacklist entry/i)).toBeInTheDocument();
    expect(screen.getByText('moonshotai/kimi-k3')).toBeInTheDocument();
  });
});

describe('AdminModelSelectionPage - preview meta', () => {
  it('names the headroom the shown preview was computed with while the field is invalid', async () => {
    await renderLoaded();
    expect(screen.getByText('headroom 1.5× · favorites and blacklist applied')).toBeInTheDocument();

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Price headroom' }), { target: { value: '0.5' } });

    // No request fires for 0.5, so the preview on screen is still the 1.5 one
    // and the meta must say so rather than claim a band it never used.
    expect(screen.getByText('headroom 1.5× · favorites and blacklist applied')).toBeInTheDocument();
    expect(screen.queryByText(/headroom 0\.5×/)).not.toBeInTheDocument();

    fireEvent.change(screen.getByRole('spinbutton', { name: 'Price headroom' }), { target: { value: '2' } });

    // A valid draft shows immediately; the preview for it is on its way.
    expect(screen.getByText('headroom 2× · favorites and blacklist applied')).toBeInTheDocument();
  });
});

describe('AdminModelSelectionPage - context menu', () => {
  it('right-clicking a ladder pill and adding it posts the slug, refetches the catalog and blacklist, and re-runs the preview', async () => {
    mocks.adminSelectorCandidates.mockResolvedValueOnce(catalogRes()).mockResolvedValueOnce({ ...catalogRes(), blacklist: ['c/weak', 'a/mid'] });
    mocks.adminBlacklistModel.mockResolvedValue({ slug: 'a/mid' });

    await renderLoaded();
    expect(screen.getByTestId('tl-dot-coder-a/mid')).not.toHaveClass('banned');

    fireEvent.contextMenu(screen.getByTestId('tl-dot-coder-a/mid'), { clientX: 50, clientY: 60 });
    const menu = screen.getByRole('menu', { name: 'a/mid' });
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Add to blacklist' }));

    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    await waitFor(() => expect(mocks.adminBlacklistModel).toHaveBeenCalledWith('a/mid'));
    await waitFor(() => expect(mocks.adminModelBlacklist).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(mocks.adminSelectorCandidates).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByTestId('tl-dot-coder-a/mid')).toHaveClass('banned'));
    await waitFor(() => expect(mocks.adminSelectorPreview).toHaveBeenCalledTimes(2));
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();
  });

  it('right-clicking a struck pill offers delist, which goes through the confirm dialog', async () => {
    mocks.adminDelistModel.mockResolvedValue({ deleted: 'c/weak' });

    await renderLoaded();

    fireEvent.contextMenu(screen.getByTestId('tl-dot-reviewer-c/weak'), { clientX: 50, clientY: 60 });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from blacklist' }));

    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('c/weak');
    expect(mocks.adminDelistModel).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /delist/i }));

    await waitFor(() => expect(mocks.adminDelistModel).toHaveBeenCalledWith('c/weak'));
    await waitFor(() => expect(mocks.adminSelectorCandidates).toHaveBeenCalledTimes(2));
    expect(mocks.adminBlacklistModel).not.toHaveBeenCalled();
  });

  it('right-clicking a preview seat opens the same menu for that model', async () => {
    await renderLoaded();
    await waitFor(() => expect(screen.getByTestId('tl-seat-complex-1')).toBeInTheDocument());

    fireEvent.contextMenu(screen.getByTestId('tl-seat-complex-1'), { clientX: 5, clientY: 6 });

    expect(screen.getByRole('menu', { name: 'b/pricey' })).toBeInTheDocument();
    expect(screen.getByRole('menuitem', { name: 'Add to blacklist' })).toBeInTheDocument();
  });

  it('surfaces a failed add as an inline error and leaves the pill unstruck', async () => {
    mocks.adminBlacklistModel.mockRejectedValue({ code: 'INTERNAL_ERROR', error: 'failed to add blacklist entry' });

    await renderLoaded();

    fireEvent.contextMenu(screen.getByTestId('tl-dot-coder-a/mid'), { clientX: 50, clientY: 60 });
    fireEvent.click(screen.getByRole('menuitem', { name: 'Add to blacklist' }));

    expect(await screen.findByText(/failed to add blacklist entry/i)).toBeInTheDocument();
    expect(screen.getByTestId('tl-dot-coder-a/mid')).not.toHaveClass('banned');
  });
});
