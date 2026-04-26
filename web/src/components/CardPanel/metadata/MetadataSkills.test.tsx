import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { api } from '../../../api/client';
import type { Card, ProjectConfig } from '../../../types';
import { MetadataSkills } from './MetadataSkills';

const mockSkills = [
  { name: 'go-development', description: 'Go development patterns' },
  { name: 'typescript-react', description: 'React/TypeScript patterns' },
  { name: 'security-review', description: 'Security review patterns' },
];

function makeCard(overrides: Partial<Card> = {}): Card {
  return {
    id: 'TEST-001',
    title: 'Test Card',
    project: 'test',
    type: 'task',
    state: 'todo',
    priority: 'medium',
    created: '2026-01-01T00:00:00Z',
    updated: '2026-01-01T00:00:00Z',
    body: '',
    ...overrides,
  };
}

function makeConfig(overrides: Partial<ProjectConfig> = {}): ProjectConfig {
  return {
    name: 'test',
    prefix: 'TEST',
    next_id: 1,
    states: ['todo', 'in_progress', 'done'],
    types: ['task'],
    priorities: ['low', 'medium', 'high'],
    transitions: {},
    ...overrides,
  };
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe('MetadataSkills — initial radio state', () => {
  it('editedCard.skills=null → "Use project default" radio is checked', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const card = makeCard();
    render(
      <MetadataSkills
        card={card}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );
    const radio = screen.getByRole('radio', { name: /Use project default/i });
    expect(radio).toBeChecked();
  });

  it('editedCard.skills=[] → "Mount no skills" radio is checked', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const card = makeCard();
    render(
      <MetadataSkills
        card={card}
        editedCard={makeCard({ skills: [] })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );
    const radio = screen.getByRole('radio', { name: /Mount no skills/i });
    expect(radio).toBeChecked();
  });

  it('editedCard.skills=["go-development"] → "Specific skills" radio is checked', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const card = makeCard();
    render(
      <MetadataSkills
        card={card}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );
    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    expect(radio).toBeChecked();
  });
});

describe('MetadataSkills — "Use project default" label varies with config.default_skills', () => {
  it('config.default_skills=null → label shows "(mount full set)"', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: null })}
        onSkillsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Use project default \(mount full set\)/i)).toBeInTheDocument();
  });

  it('config.default_skills=undefined → label shows "(mount full set)"', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: undefined })}
        onSkillsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Use project default \(mount full set\)/i)).toBeInTheDocument();
  });

  it('config.default_skills=[] → label shows "(mount nothing)"', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: [] })}
        onSkillsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Use project default \(mount nothing\)/i)).toBeInTheDocument();
  });

  it('config.default_skills=[one skill] → label shows "(1 skill)"', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: ['go-development'] })}
        onSkillsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Use project default \(1 skill\)/i)).toBeInTheDocument();
  });

  it('config.default_skills=[two skills] → label shows "(2 skills)"', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: ['go-development', 'typescript-react'] })}
        onSkillsChange={vi.fn()}
      />,
    );
    expect(screen.getByText(/Use project default \(2 skills\)/i)).toBeInTheDocument();
  });
});

describe('MetadataSkills — bug-anchor: clicking "Specific skills" while in inherit/none mode', () => {
  it('clicking "Specific skills" from null keeps radio checked and does NOT call onSkillsChange', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    fireEvent.click(radio);

    expect(radio).toBeChecked();
    expect(onSkillsChange).not.toHaveBeenCalled();
  });

  it('clicking "Specific skills" from null reveals the checkbox list', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );

    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    fireEvent.click(radio);

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });
  });

  it('clicking "Specific skills" from [] keeps radio checked and does NOT call onSkillsChange', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: [] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    fireEvent.click(radio);

    expect(radio).toBeChecked();
    expect(onSkillsChange).not.toHaveBeenCalled();
  });

  it('clicking "Specific skills" from [] reveals the checkbox list', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: [] })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );

    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    fireEvent.click(radio);

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });
  });
});

describe('MetadataSkills — checkbox list subset constraint', () => {
  it('when config.default_skills is non-empty, checkbox list is constrained to that subset', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig({ default_skills: ['go-development'] })}
        onSkillsChange={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });
    // typescript-react is not in default_skills, so should NOT appear
    expect(screen.queryByRole('checkbox', { name: /typescript-react/i })).not.toBeInTheDocument();
  });

  it('when config.default_skills is null, checkbox list shows the full available set', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig({ default_skills: null })}
        onSkillsChange={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: /typescript-react/i })).toBeInTheDocument();
      expect(screen.getByRole('checkbox', { name: /security-review/i })).toBeInTheDocument();
    });
  });
});

describe('MetadataSkills — "Specific skills" disabled when config.default_skills=[]', () => {
  it('"Specific skills" radio is disabled when config.default_skills is empty', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig({ default_skills: [] })}
        onSkillsChange={vi.fn()}
      />,
    );
    const radio = screen.getByRole('radio', { name: /Specific skills/i });
    expect(radio).toBeDisabled();
  });
});

describe('MetadataSkills — mode-switch behaviour', () => {
  it('clicking "Use project default" calls onSkillsChange(null)', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: [] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    fireEvent.click(screen.getByRole('radio', { name: /Use project default/i }));
    expect(onSkillsChange).toHaveBeenCalledWith(null);
  });

  it('clicking "Mount no skills" calls onSkillsChange([])', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    fireEvent.click(screen.getByRole('radio', { name: /Mount no skills/i }));
    expect(onSkillsChange).toHaveBeenCalledWith([]);
  });
});

describe('MetadataSkills — toggling a checkbox', () => {
  it('toggling a checkbox in Specific mode calls onSkillsChange with sorted array', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /typescript-react/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('checkbox', { name: /typescript-react/i }));
    expect(onSkillsChange).toHaveBeenCalledWith(['go-development', 'typescript-react']);
  });

  it('unchecking a skill calls onSkillsChange with sorted array minus the skill', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();
    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development', 'typescript-react'] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole('checkbox', { name: /go-development/i })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('checkbox', { name: /go-development/i }));
    expect(onSkillsChange).toHaveBeenCalledWith(['typescript-react']);
  });
});

describe('MetadataSkills — loading and error states', () => {
  it('shows loading state while getTaskSkills is pending (in specific mode)', async () => {
    let resolve: (v: typeof mockSkills) => void;
    const promise = new Promise<typeof mockSkills>(r => { resolve = r; });
    vi.spyOn(api, 'getTaskSkills').mockReturnValue(promise);

    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );

    expect(screen.getByText(/Loading/i)).toBeInTheDocument();

    resolve!(mockSkills);
    await waitFor(() => {
      expect(screen.queryByText(/Loading/i)).not.toBeInTheDocument();
    });
  });

  it('shows error state when getTaskSkills rejects', async () => {
    vi.spyOn(api, 'getTaskSkills').mockRejectedValue({ error: 'Failed to fetch' });

    render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig()}
        onSkillsChange={vi.fn()}
      />,
    );

    await waitFor(() => {
      expect(screen.getByText(/Failed to fetch/i)).toBeInTheDocument();
    });
  });
});

describe('MetadataSkills — external value change re-derives localMode', () => {
  it('rerender with value=["go-development"] while in specific mode keeps localMode=specific', async () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();

    // Start with null (inherit mode)
    const { rerender } = render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    // Click "Specific skills" — localMode becomes 'specific'
    fireEvent.click(screen.getByRole('radio', { name: /Specific skills/i }));
    expect(screen.getByRole('radio', { name: /Specific skills/i })).toBeChecked();

    // Rerender with value=['go-development'] — modeFor(['go-development']) = 'specific'
    // prevValue changes so localMode re-derives to 'specific' (same, no visual change)
    rerender(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: ['go-development'] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );
    expect(screen.getByRole('radio', { name: /Specific skills/i })).toBeChecked();
  });

  it('rerender with value=[] after clicking "Specific skills" flips localMode to none', () => {
    vi.spyOn(api, 'getTaskSkills').mockResolvedValue(mockSkills);
    const onSkillsChange = vi.fn();

    // Start with null (inherit mode)
    const { rerender } = render(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: null })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );

    // Click "Specific skills" — localMode becomes 'specific'
    fireEvent.click(screen.getByRole('radio', { name: /Specific skills/i }));
    expect(screen.getByRole('radio', { name: /Specific skills/i })).toBeChecked();

    // Rerender with value=[] — modeFor([]) = 'none', prevValue changes, so localMode flips to 'none'
    rerender(
      <MetadataSkills
        card={makeCard()}
        editedCard={makeCard({ skills: [] })}
        config={makeConfig()}
        onSkillsChange={onSkillsChange}
      />,
    );
    expect(screen.getByRole('radio', { name: /Mount no skills/i })).toBeChecked();
  });
});
