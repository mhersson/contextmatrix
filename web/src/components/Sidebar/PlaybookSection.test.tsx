import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PlaybookSection } from './PlaybookSection';
import { PlaybooksProvider } from '../../hooks/usePlaybooks';
import { api } from '../../api/client';
import type { PlaybookRunStatus, PlaybookSummary } from '../../types';

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, reconnectEpoch: 0 }),
}));
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: true, toggle: () => {}, close: () => {} }),
}));

function pb(id: string, o: { run?: PlaybookRunStatus; pinned?: boolean; complete?: number; day?: number } = {}): PlaybookSummary {
  return {
    id,
    title: `Title ${id}`,
    complete: o.complete ?? 0,
    total: 4,
    segments: [],
    projects: 1,
    project_names: [],
    run_status: o.run,
    pinned: o.pinned,
    updated_at: `2026-10-${String(o.day ?? 1).padStart(2, '0')}T12:00:00Z`,
  };
}

const KEY = 'sidebar.playbook_section_collapsed';

function renderSection() {
  return render(
    <MemoryRouter>
      <PlaybooksProvider>
        <PlaybookSection />
      </PlaybooksProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  localStorage.clear();
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe('PlaybookSection', () => {
  it('shows at most three rows and the total on the All link, which has no + new beside it', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([1, 2, 3, 4, 5].map((d) => pb(`p${d}`, { day: d })));
    renderSection();
    const section = await screen.findByRole('region', { name: 'Playbooks' });
    await within(section).findByText('Title p5');
    expect(within(section).getAllByRole('listitem')).toHaveLength(3);
    const all = within(section).getByRole('link', { name: /All playbooks/ });
    expect(all).toHaveAttribute('href', '/playbooks');
    expect(all).toHaveTextContent('5');
    expect(within(section).queryByRole('button', { name: /^New / })).not.toBeInTheDocument();
  });

  it('reads n of m while running and the status word otherwise', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([
      pb('run', { run: 'running', complete: 1, day: 3 }),
      pb('wait', { run: 'waiting', day: 2 }),
      pb('idle', { day: 1 }),
    ]);
    renderSection();
    const section = await screen.findByRole('region', { name: 'Playbooks' });
    const rowFor = async (title: string) => (await within(section).findByText(title)).closest('li')!;
    expect(await rowFor('Title run')).toHaveTextContent('1 of 4');
    expect(await rowFor('Title wait')).toHaveTextContent('waiting');
    expect(await rowFor('Title idle')).toHaveTextContent('not started');
  });

  it('marks a pinned playbook and offers no pin button', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a', { pinned: true }), pb('b')]);
    renderSection();
    const section = await screen.findByRole('region', { name: 'Playbooks' });
    const pinned = (await within(section).findByText('Title a')).closest('li')!;
    expect(within(pinned).getByRole('img', { name: 'Pinned' })).toBeInTheDocument();
    expect(within(section).queryByRole('button', { name: /pin/i })).not.toBeInTheDocument();
  });

  it('opens a playbook at its detail page', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
    renderSection();
    const link = (await screen.findByText('Title a')).closest('a')!;
    expect(link).toHaveAttribute('href', '/playbooks/a');
  });

  it('remembers the collapse under its own key', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
    const first = renderSection();
    await screen.findByText('Title a');
    fireEvent.click(screen.getByRole('button', { name: /Playbooks/ }));
    expect(localStorage.getItem(KEY)).toBe('1');
    expect(screen.queryByText('Title a')).not.toBeInTheDocument();
    first.unmount();
    renderSection();
    expect(await screen.findByRole('button', { name: /Playbooks/ })).toHaveAttribute('aria-expanded', 'false');
  });
});
