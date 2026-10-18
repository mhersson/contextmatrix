import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { TableSection, type Column } from './TableSection';

interface Item {
  id: string;
  text: string;
  basis: string;
}

const items: Item[] = [
  { id: 'A1', text: 'Clients ignore whitespace.', basis: 'Q1' },
  { id: 'A2', text: 'Two-space indentation.', basis: 'Q2' },
];

const columns: Column<Item>[] = [
  { label: 'Id', kind: 'id', cell: (a) => a.id },
  { label: 'Assumption', kind: 'long', cell: (a) => a.text },
  { label: 'Basis', kind: 'short', cell: (a) => a.basis },
];

const rowOf = (text: string) => screen.getByText(text).closest('tr')!;

describe('TableSection', () => {
  it('heads the table with its title and row count, and gives each item a row under the column headers', () => {
    render(<TableSection title="Assumptions" columns={columns} rows={items} rowKey={(a) => a.id} />);
    const heading = screen.getByRole('heading', { name: /Assumptions/ });
    expect(within(heading).getByText('2')).toBeInTheDocument();
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent)).toEqual(['Id', 'Assumption', 'Basis']);
    const cells = within(rowOf('Two-space indentation.')).getAllByRole('cell');
    expect(cells.map((c) => c.textContent)).toEqual(['A2', 'Two-space indentation.', 'Q2']);
  });

  it('draws nothing without rows', () => {
    const { container } = render(<TableSection title="Assumptions" columns={columns} rows={[]} rowKey={(a) => a.id} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('marks only the changed rows with the revision', () => {
    render(<TableSection title="Assumptions" columns={columns} rows={items} rowKey={(a) => a.id} changed={new Set(['A2'])} version={3} />);
    expect(within(rowOf('Two-space indentation.')).getByText(/rev 3/)).toBeInTheDocument();
    expect(within(rowOf('Clients ignore whitespace.')).queryByText(/rev 3/)).not.toBeInTheDocument();
  });
});
