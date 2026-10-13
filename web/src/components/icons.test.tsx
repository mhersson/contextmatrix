import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { sourceIcon } from './icons';

describe('sourceIcon', () => {
  it.each([
    ['github', 'Imported from GitHub'],
    ['jira', 'Imported from Jira'],
  ])('renders the %s icon', (system, label) => {
    render(<>{sourceIcon(system)}</>);
    expect(screen.getByRole('img', { name: label })).toBeInTheDocument();
  });

  it('renders nothing for an unknown or missing system', () => {
    const { container } = render(<>{sourceIcon('linear')}{sourceIcon(undefined)}</>);
    expect(container).toBeEmptyDOMElement();
  });
});
