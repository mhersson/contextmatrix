import { describe, it, expect, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { GitHubImportSection } from './GitHubImportSection';

const types = ['task', 'bug'];
const priorities = ['low', 'high'];

describe('GitHubImportSection - a current value the board no longer lists', () => {
  it.each([
    { label: 'Card type', github: { import_issues: true, card_type: 'epic' }, field: 'card_type', value: 'epic' },
    {
      label: 'Default priority',
      github: { import_issues: true, default_priority: 'medium' },
      field: 'default_priority',
      value: 'medium',
    },
  ])('$label keeps $value selected and lets Board default clear it', ({ label, github, field, value }) => {
    const onChange = vi.fn();
    render(<GitHubImportSection github={github} onChange={onChange} types={types} priorities={priorities} />);

    const select = screen.getByLabelText(label) as HTMLSelectElement;
    expect(select.value).toBe(value);
    expect(select.selectedOptions[0].value).toBe(value);

    fireEvent.change(select, { target: { value: '' } });
    expect(onChange).toHaveBeenCalledWith({ ...github, [field]: undefined });
  });

  it('adds no extra option when the current value is listed', () => {
    render(
      <GitHubImportSection
        github={{ import_issues: true, card_type: 'bug' }}
        onChange={vi.fn()}
        types={types}
        priorities={priorities}
      />,
    );

    const select = screen.getByLabelText('Card type') as HTMLSelectElement;
    expect(select.value).toBe('bug');
    expect(Array.from(select.options).map((o) => o.value)).toEqual(['', 'task', 'bug']);
  });
});
