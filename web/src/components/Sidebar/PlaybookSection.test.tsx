import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { PlaybookSection } from './PlaybookSection';
import { SessionsPage } from '../Sessions/SessionsPage';
import { PlaybooksProvider } from '../../hooks/usePlaybooks';
import { api } from '../../api/client';
import type { PlaybookRunStatus, PlaybookSummary } from '../../types';

vi.mock('../../hooks/useSSEBus', () => ({
  useSSEBus: () => ({ subscribe: () => () => {}, connected: true, reconnectEpoch: 0 }),
}));
vi.mock('../../context/MobileSidebarContext', () => ({
  useMobileSidebar: () => ({ isOpen: true, toggle: () => {}, close: () => {} }),
}));
const toast = vi.hoisted(() => ({ showToast: () => {} }));
vi.mock('../../hooks/useToast', () => ({ useToast: () => toast }));
vi.mock('../../hooks/useTheme', () => ({ useTheme: () => ({ boardsRepos: [] }) }));

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

function Where() {
  const loc = useLocation();
  return <div data-testid="where">{loc.pathname + loc.search}</div>;
}

describe('PlaybookSection', () => {
  it('+ new lands on the Playbooks tab with the create row open', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
    vi.spyOn(api, 'listChats').mockResolvedValue([]);
    vi.spyOn(api, 'getJiraStatus').mockResolvedValue({ configured: false });
    render(
      <MemoryRouter initialEntries={['/']}>
        <PlaybooksProvider>
          <PlaybookSection />
          <Routes>
            <Route path="/sessions" element={<SessionsPage />} />
            <Route path="*" element={null} />
          </Routes>
          <Where />
        </PlaybooksProvider>
      </MemoryRouter>,
    );
    const section = await screen.findByRole('region', { name: 'Playbooks' });
    fireEvent.click(within(section).getByRole('button', { name: 'New playbook' }));

    expect(await screen.findByLabelText('Playbook title')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /Playbooks/ })).toHaveAttribute('aria-selected', 'true');
    await waitFor(() => expect(screen.getByTestId('where')).toHaveTextContent(/^\/sessions\?tab=playbooks$/));
  });

  it('shows at most three rows and the total on the All link, which opens the Playbooks tab', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([1, 2, 3, 4, 5].map((d) => pb(`p${d}`, { day: d })));
    renderSection();
    const section = await screen.findByRole('region', { name: 'Playbooks' });
    await within(section).findByText('Title p5');
    expect(within(section).getAllByRole('listitem')).toHaveLength(3);
    const all = within(section).getByRole('link', { name: /All playbooks/ });
    expect(all).toHaveAttribute('href', '/sessions?tab=playbooks');
    expect(all).toHaveTextContent('5');
  });

  it.each([
    ['running', 'var(--aqua)', { run: 'running', complete: 1 }],
    ['waiting', 'var(--wait-yellow)', { run: 'waiting' }],
    ['stopped', 'var(--orange)', { run: 'stopped', complete: 2 }],
    ['not started', 'var(--grey0)', {}],
    ['completed', 'var(--grey0)', { complete: 4 }],
  ] as const)('leads with a %s dot and ends with the last activity, with no status pill', async (state, color, o) => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a', o)]);
    renderSection();
    const row = (await screen.findByText('Title a')).closest('li')!;
    const dot = within(row).getByRole('img', { name: state });
    expect(dot).toHaveAttribute('title', state);
    expect(dot.style.backgroundColor).toBe(color);
    expect(row.firstElementChild!.firstElementChild).toBe(dot);
    const age = row.querySelector('.sb-age')!;
    expect(age).toHaveAttribute('title', new Date('2026-10-01T12:00:00Z').toLocaleString());
    expect(age).toHaveTextContent(/^\d+[dh]$/);
    expect(age).toHaveAttribute('aria-hidden', 'true');
    const link = row.querySelector('a')!;
    const slot = link.lastElementChild!;
    expect(slot).toHaveClass('sb-pin');
    expect(slot).toHaveAttribute('aria-hidden', 'true');
    expect(slot).toHaveTextContent('');
    expect(slot).not.toHaveClass('sb-pin--shown');
    expect(slot.previousElementSibling!.previousElementSibling).toBe(age);
  });

  it('keeps the compact age out of the accessible name and reads the relative time instead', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
    renderSection();
    const link = (await screen.findByText('Title a')).closest('a')!;
    expect(link).toHaveAccessibleName(/Title a.* ago$/);
    expect(link).not.toHaveAccessibleName(/\d+[dhm]$/);
  });

  it('shows a pinned playbook pin in the trailing slot after the age', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a', { pinned: true })]);
    renderSection();
    const link = (await screen.findByText('Title a')).closest('a')!;
    const slot = link.lastElementChild!;
    expect(slot).toHaveClass('sb-pin', 'sb-pin--shown');
    expect(slot).not.toHaveAttribute('aria-hidden');
    expect(within(slot as HTMLElement).getByRole('img', { name: 'Pinned' })).toBeInTheDocument();
    expect(link.querySelector('.sb-age')!.compareDocumentPosition(slot) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('colours the title like a session row, not as a link', async () => {
    vi.spyOn(api, 'listPlaybooks').mockResolvedValue([pb('a')]);
    renderSection();
    const link = (await screen.findByText('Title a')).closest('a')!;
    expect(link).toHaveClass('sb-playbook-row');
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
