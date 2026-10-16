import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { BlueprintSessionForm } from './BlueprintSessionForm';

const projects = [
  { name: 'alpha', display_name: 'Alpha' },
  { name: 'beta' },
];

describe('BlueprintSessionForm', () => {
  it('toggles projects and reports the selection', () => {
    const onChange = vi.fn();
    render(
      <BlueprintSessionForm
        projects={projects}
        value={{ projects: [], mode: 'default', costCapUSD: '' }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByLabelText('Alpha'));
    expect(onChange).toHaveBeenCalledWith({ projects: ['alpha'], mode: 'default', costCapUSD: '' });
  });

  it('unticks a selected project', () => {
    const onChange = vi.fn();
    render(
      <BlueprintSessionForm
        projects={projects}
        value={{ projects: ['alpha', 'beta'], mode: 'default', costCapUSD: '' }}
        onChange={onChange}
      />,
    );
    fireEvent.click(screen.getByLabelText('beta'));
    expect(onChange).toHaveBeenCalledWith({ projects: ['alpha'], mode: 'default', costCapUSD: '' });
  });

  it('offers the four modes and reports a change', () => {
    const onChange = vi.fn();
    render(
      <BlueprintSessionForm
        projects={projects}
        value={{ projects: ['alpha'], mode: 'default', costCapUSD: '' }}
        onChange={onChange}
      />,
    );
    const select = screen.getByLabelText('Mode');
    expect(screen.getAllByRole('option').map((o) => (o as HTMLOptionElement).value)).toEqual([
      'default',
      'auto',
      'auto+emit',
      'yolo',
    ]);
    fireEvent.change(select, { target: { value: 'yolo' } });
    expect(onChange).toHaveBeenCalledWith({ projects: ['alpha'], mode: 'yolo', costCapUSD: '' });
  });

  it('reports the cost cap as typed', () => {
    const onChange = vi.fn();
    render(
      <BlueprintSessionForm
        projects={projects}
        value={{ projects: ['alpha'], mode: 'default', costCapUSD: '' }}
        onChange={onChange}
      />,
    );
    fireEvent.change(screen.getByLabelText(/Cost cap/), { target: { value: '12.5' } });
    expect(onChange).toHaveBeenCalledWith({ projects: ['alpha'], mode: 'default', costCapUSD: '12.5' });
  });
});
