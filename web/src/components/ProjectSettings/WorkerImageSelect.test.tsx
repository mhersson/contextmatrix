import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { WorkerImageSelect } from './WorkerImageSelect';

const mocks = vi.hoisted(() => ({
  getBackendImages: vi.fn(),
}));

vi.mock('../../api/client', async (importOriginal) => {
  const orig = await importOriginal<typeof import('../../api/client')>();
  return {
    ...orig,
    api: {
      ...orig.api,
      getBackendImages: mocks.getBackendImages,
    },
  };
});

const inputStyle = {};

beforeEach(() => {
  vi.resetAllMocks();
});

function renderSelect(overrides: Partial<Parameters<typeof WorkerImageSelect>[0]> = {}) {
  return render(
    <WorkerImageSelect
      backend="agent"
      label="Worker image"
      value=""
      onChange={vi.fn()}
      readOnly={false}
      inputStyle={inputStyle}
      {...overrides}
    />,
  );
}

describe('WorkerImageSelect', () => {
  it('renders "Backend default" plus one option per fetched tag', async () => {
    mocks.getBackendImages.mockResolvedValue({
      ok: true,
      images: [
        { tags: ['contextmatrix-agent-worker:go-node', 'contextmatrix-agent-worker:dev'] },
        { tags: ['ghcr.io/mhersson/contextmatrix-agent:python'] },
      ],
    });

    renderSelect();
    await waitFor(() => expect(mocks.getBackendImages).toHaveBeenCalledWith('agent'));

    expect(await screen.findByRole('option', { name: /backend default/i })).toBeInTheDocument();
    expect(
      await screen.findByRole('option', { name: 'contextmatrix-agent-worker:go-node' }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole('option', { name: 'contextmatrix-agent-worker:dev' }),
    ).toBeInTheDocument();
    expect(
      await screen.findByRole('option', { name: 'ghcr.io/mhersson/contextmatrix-agent:python' }),
    ).toBeInTheDocument();
  });

  it('keeps a saved value missing from the list selectable, with a warning', async () => {
    mocks.getBackendImages.mockResolvedValue({
      ok: true,
      images: [{ tags: ['contextmatrix-agent-worker:go-node'] }],
    });

    renderSelect({ value: 'contextmatrix-agent-worker:pruned' });
    await waitFor(() => expect(mocks.getBackendImages).toHaveBeenCalled());

    expect(
      await screen.findByRole('option', {
        name: 'contextmatrix-agent-worker:pruned (not on worker node)',
      }),
    ).toBeInTheDocument();
    expect(await screen.findByRole('alert')).toHaveTextContent(/not present on the worker node/i);
  });

  it('recognizes a digest-pinned saved value via the image digests, no warning', async () => {
    mocks.getBackendImages.mockResolvedValue({
      ok: true,
      images: [
        {
          tags: ['contextmatrix-agent-worker:go-node'],
          digests: ['contextmatrix-agent-worker@sha256:abc123'],
        },
      ],
    });

    renderSelect({ value: 'contextmatrix-agent-worker@sha256:abc123' });
    await waitFor(() => expect(mocks.getBackendImages).toHaveBeenCalled());

    expect(
      await screen.findByRole('option', { name: 'contextmatrix-agent-worker@sha256:abc123' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('combobox')).toHaveValue('contextmatrix-agent-worker@sha256:abc123');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('shows the saved value as a selectable option before the fetch resolves', () => {
    let resolveFetch: (value: { ok: true; images: never[] }) => void = () => {};
    mocks.getBackendImages.mockReturnValue(
      new Promise((resolve) => {
        resolveFetch = resolve;
      }),
    );

    renderSelect({ value: 'contextmatrix-agent-worker:pinned' });

    expect(
      screen.getByRole('option', { name: 'contextmatrix-agent-worker:pinned' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('combobox')).toHaveValue('contextmatrix-agent-worker:pinned');

    // Resolve so the pending promise doesn't leak state updates into later tests.
    resolveFetch({ ok: true, images: [] });
  });

  it('degrades to Backend default + saved value with a notice when the fetch fails', async () => {
    mocks.getBackendImages.mockRejectedValue({ error: 'backend images probe failed', code: 'BACKEND_UNAVAILABLE' });

    renderSelect({ value: 'contextmatrix-agent-worker:go-node' });
    await waitFor(() => expect(mocks.getBackendImages).toHaveBeenCalled());

    expect(await screen.findByRole('option', { name: /backend default/i })).toBeInTheDocument();
    expect(
      await screen.findByRole('option', { name: 'contextmatrix-agent-worker:go-node' }),
    ).toBeInTheDocument();
    expect(await screen.findByText(/could not load the image list/i)).toBeInTheDocument();
  });

  it('readOnly renders plain text and skips the fetch', () => {
    renderSelect({ readOnly: true, value: 'contextmatrix-agent-worker:go-node' });

    expect(mocks.getBackendImages).not.toHaveBeenCalled();
    expect(screen.getByText('contextmatrix-agent-worker:go-node')).toBeInTheDocument();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
  });

  it('readOnly with no value shows Backend default', () => {
    renderSelect({ readOnly: true, value: '' });

    expect(screen.getByText(/backend default/i)).toBeInTheDocument();
  });
});
