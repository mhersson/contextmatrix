import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SortMenu } from './SortMenu';

describe('SortMenu', () => {
  const onChange = vi.fn();

  beforeEach(() => {
    onChange.mockClear();
  });

  it('renders the trigger button', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    const button = screen.getByRole('button', { name: /sort/i });
    expect(button).toBeTruthy();
  });

  it('does not show sort options when closed', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    expect(screen.queryByText('Recent')).toBeNull();
    expect(screen.queryByText('Priority')).toBeNull();
  });

  it('opens dropdown on trigger click', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));

    expect(screen.getByText('Recent')).toBeTruthy();
    expect(screen.getByText('ID ↑')).toBeTruthy();
    expect(screen.getByText('ID ↓')).toBeTruthy();
    expect(screen.getByText('Priority')).toBeTruthy();
    expect(screen.getByText('Type')).toBeTruthy();
  });

  it('calls onChange with the correct mode when an item is clicked', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));

    fireEvent.click(screen.getByText('Priority'));
    expect(onChange).toHaveBeenCalledWith('priority');
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it('calls onChange with "id-asc" when ID ↑ is clicked', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    fireEvent.click(screen.getByText('ID ↑'));
    expect(onChange).toHaveBeenCalledWith('id-asc');
  });

  it('calls onChange with "id-desc" when ID ↓ is clicked', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    fireEvent.click(screen.getByText('ID ↓'));
    expect(onChange).toHaveBeenCalledWith('id-desc');
  });

  it('calls onChange with "type" when Type is clicked', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    fireEvent.click(screen.getByText('Type'));
    expect(onChange).toHaveBeenCalledWith('type');
  });

  it('closes the dropdown after an item is selected', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    expect(screen.getByText('Priority')).toBeTruthy();

    fireEvent.click(screen.getByText('Priority'));
    expect(screen.queryByText('Priority')).toBeNull();
  });

  it('closes the dropdown on Escape key', () => {
    render(<SortMenu current="recent" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    expect(screen.getByText('Priority')).toBeTruthy();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('Priority')).toBeNull();
  });

  it('closes the dropdown on outside click', () => {
    render(
      <div>
        <SortMenu current="recent" onChange={onChange} />
        <button type="button">outside</button>
      </div>
    );

    fireEvent.click(screen.getByRole('button', { name: /sort/i }));
    expect(screen.getByText('Priority')).toBeTruthy();

    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByText('Priority')).toBeNull();
  });

  it('marks the active mode visually', () => {
    render(<SortMenu current="priority" onChange={onChange} />);
    fireEvent.click(screen.getByRole('button', { name: /sort/i }));

    // Active mode has a dot marker (●) and bold text
    const priorityOption = screen.getByText('Priority').closest('button');
    expect(priorityOption?.textContent).toContain('●');

    // Inactive mode does not
    const recentOption = screen.getByText('Recent').closest('button');
    expect(recentOption?.textContent).not.toContain('●');
  });
});
