import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { KindTiles } from './KindTiles';
import { ProjectList } from './ProjectList';
import { BlueprintFields } from './BlueprintFields';
import { EMPTY_BLUEPRINT_FORM } from './blueprintForm';

vi.mock('../../hooks/useTheme', () => ({ useTheme: () => ({ favorites: null }) }));

const projects = [
  { name: 'alpha', display_name: 'Alpha', repo: 'git@github.com:example/alpha.git' },
  { name: 'beta' },
];

describe('KindTiles', () => {
  it('reports the picked kind', () => {
    const onChange = vi.fn();
    render(<KindTiles blueprint={false} onChange={onChange} />);
    expect(screen.getByRole('radio', { name: 'Chat' })).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(screen.getByRole('radio', { name: 'Blueprint' }));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('switches kind with an arrow key', () => {
    const onChange = vi.fn();
    render(<KindTiles blueprint onChange={onChange} />);
    fireEvent.keyDown(screen.getByRole('radio', { name: 'Blueprint' }), { key: 'ArrowLeft' });
    expect(onChange).toHaveBeenCalledWith(false);
  });
});

describe('ProjectList', () => {
  it('toggles projects in multi-select and shows each repo', () => {
    const onChange = vi.fn();
    render(<ProjectList projects={projects} multi selected={['beta']} onChange={onChange} />);
    expect(screen.getByText('example/alpha')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('checkbox', { name: 'Alpha' }));
    expect(onChange).toHaveBeenCalledWith(['beta', 'alpha']);
    fireEvent.click(screen.getByRole('checkbox', { name: 'beta' }));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  it('offers Across projects first in single-select', () => {
    const onChange = vi.fn();
    render(<ProjectList projects={projects} multi={false} selected={['alpha']} onChange={onChange} />);
    const radios = screen.getAllByRole('radio');
    expect(radios[0]).toHaveAccessibleName('Across projects');
    expect(screen.getByRole('radio', { name: 'Alpha' })).toBeChecked();
    fireEvent.click(radios[0]);
    expect(onChange).toHaveBeenCalledWith(['']);
  });
});

const noModels = { source: 'endpoint' as const, model: '', defaultModel: '', models: [], onChange: () => {} };

describe('BlueprintFields', () => {
  it('counts the picked projects', () => {
    render(<BlueprintFields projects={projects} value={{ ...EMPTY_BLUEPRINT_FORM, projects: ['alpha', 'beta'] }} onChange={() => {}} models={noModels} />);
    expect(screen.getByText('2 selected')).toBeInTheDocument();
  });

  it('reports a mode and a cap preset', () => {
    const onChange = vi.fn();
    render(<BlueprintFields projects={projects} value={EMPTY_BLUEPRINT_FORM} onChange={onChange} models={noModels} />);
    expect(screen.getByRole('radio', { name: 'No cap' })).toBeChecked();
    fireEvent.click(screen.getByRole('radio', { name: 'Yolo' }));
    expect(onChange).toHaveBeenCalledWith({ ...EMPTY_BLUEPRINT_FORM, mode: 'yolo' });
    fireEvent.click(screen.getByRole('radio', { name: '$5' }));
    expect(onChange).toHaveBeenCalledWith({ ...EMPTY_BLUEPRINT_FORM, capUSD: 5 });
  });

  it('orders the fields projects, mode, model, cost cap', () => {
    render(<BlueprintFields projects={projects} value={EMPTY_BLUEPRINT_FORM} onChange={() => {}} models={{ ...noModels, models: [{ id: 'm1', name: 'M1' }] as never }} />);
    const before = (a: HTMLElement, b: HTMLElement) =>
      expect(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const projectsEl = screen.getByRole('checkbox', { name: 'Alpha' });
    const modeEl = screen.getByRole('radiogroup', { name: 'Mode' });
    const modelEl = screen.getByLabelText('Model');
    const capEl = screen.getByRole('radiogroup', { name: 'Cost cap' });
    before(projectsEl, modeEl);
    before(modeEl, modelEl);
    before(modelEl, capEl);
  });
});
