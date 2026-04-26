import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { api } from '../../api/client';
import { DefaultSkillsSelector } from './DefaultSkillsSelector';

const mockSkills = [
  { name: 'go-development', description: 'Go development patterns' },
  { name: 'typescript-react', description: 'React/TypeScript patterns' },
];

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('DefaultSkillsSelector — initial radio state', () => {
  it('value=null → "Mount the full task-skills set" radio is checked', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(<DefaultSkillsSelector value={null} onChange={vi.fn()} />);
    const radio = screen.getByRole('radio', { name: /Mount the full task-skills set/i });
    expect(radio).toBeChecked();
  });

  it('value=[] → "Mount no skills" radio is checked', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(<DefaultSkillsSelector value={[]} onChange={vi.fn()} />);
    const radio = screen.getByRole('radio', { name: /Mount no skills/i });
    expect(radio).toBeChecked();
  });

  it('value=["go-development"] → "Constrain to selected skills" radio is checked', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(<DefaultSkillsSelector value={['go-development']} onChange={vi.fn()} />);
    const radio = screen.getByRole('radio', { name: /Constrain to selected skills/i });
    expect(radio).toBeChecked();
  });
});

describe('DefaultSkillsSelector — clicking "Constrain to selected skills" from inherit mode', () => {
  it('radio becomes checked and onChange is NOT called', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onChange = vi.fn();
    render(<DefaultSkillsSelector value={null} onChange={onChange} />);

    const radio = screen.getByRole('radio', { name: /Constrain to selected skills/i });
    fireEvent.click(radio);

    expect(radio).toBeChecked();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('checkboxes appear after loading when "Constrain to selected skills" is clicked', async () => {
    let resolve: (v: typeof mockSkills) => void;
    const promise = new Promise<typeof mockSkills>(r => { resolve = r; });
    vi.spyOn(api, 'getTaskSkills').mockReturnValue(promise);

    render(<DefaultSkillsSelector value={null} onChange={vi.fn()} />);

    const radio = screen.getByRole('radio', { name: /Constrain to selected skills/i });
    fireEvent.click(radio);

    // Loading state is shown while promise is pending
    expect(screen.getByText(/Loading/i)).toBeInTheDocument();

    resolve!(mockSkills);

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });
  });
});

describe('DefaultSkillsSelector — toggling a checkbox', () => {
  it('calls onChange with the updated sorted list when a checkbox is toggled', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onChange = vi.fn();
    render(<DefaultSkillsSelector value={['go-development']} onChange={onChange} />);

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /typescript-react/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('checkbox', { name: /typescript-react/i }));
    expect(onChange).toHaveBeenCalledWith(['go-development', 'typescript-react']);
  });

  it('calls onChange with sorted list when unchecking a skill', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onChange = vi.fn();
    render(<DefaultSkillsSelector value={['go-development', 'typescript-react']} onChange={onChange} />);

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('checkbox', { name: /go-development/i }));
    expect(onChange).toHaveBeenCalledWith(['typescript-react']);
  });
});

describe('DefaultSkillsSelector — clicking "Mount no skills"', () => {
  it('calls onChange([])', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onChange = vi.fn();
    render(<DefaultSkillsSelector value={null} onChange={onChange} />);

    fireEvent.click(screen.getByRole('radio', { name: /Mount no skills/i }));
    expect(onChange).toHaveBeenCalledWith([]);
  });
});

describe('DefaultSkillsSelector — clicking "Mount the full task-skills set"', () => {
  it('calls onChange(null)', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onChange = vi.fn();
    render(<DefaultSkillsSelector value={[]} onChange={onChange} />);

    fireEvent.click(screen.getByRole('radio', { name: /Mount the full task-skills set/i }));
    expect(onChange).toHaveBeenCalledWith(null);
  });
});

describe('DefaultSkillsSelector — loading state', () => {
  it('shows loading state while getTaskSkills resolves', async () => {
    let resolve: (v: typeof mockSkills) => void;
    const promise = new Promise<typeof mockSkills>(r => { resolve = r; });
    vi.spyOn(api, 'getTaskSkills').mockReturnValue(promise);

    render(<DefaultSkillsSelector value={['go-development']} onChange={vi.fn()} />);

    // In 'specific' mode, loading indicator should be visible
    expect(screen.getByText(/Loading/i)).toBeInTheDocument();

    resolve!(mockSkills);
    await waitFor(() => {
      expect(screen.queryByText(/Loading/i)).not.toBeInTheDocument();
    });
  });
});

describe('DefaultSkillsSelector — error state', () => {
  it('shows error state when getTaskSkills rejects', async () => {
    vi.spyOn(api, 'getTaskSkills').mockRejectedValue({ error: 'Failed to fetch' });

    render(<DefaultSkillsSelector value={['go-development']} onChange={vi.fn()} />);

    await waitFor(() => {
      expect(screen.getByText(/Failed to fetch/i)).toBeInTheDocument();
    });
  });
});
