import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import { PasswordInput } from './PasswordInput';

function Harness({ initial = 'hunter2-secret' }: { initial?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <PasswordInput
      label="Password"
      hint="min 10 chars"
      value={value}
      onChange={setValue}
      autoComplete="new-password"
    />
  );
}

describe('PasswordInput', () => {
  it('renders masked with an accessible show-password toggle and hint', () => {
    render(<Harness />);

    const input = screen.getByLabelText('Password');
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autocomplete', 'new-password');
    expect(screen.getByText('min 10 chars')).toBeInTheDocument();

    const toggle = screen.getByRole('button', { name: 'Show password' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    // Must never act as a submit button inside auth forms.
    expect(toggle).toHaveAttribute('type', 'button');
  });

  it('toggle reveals the value, flips its accessible name, and re-masks', () => {
    render(<Harness />);

    fireEvent.click(screen.getByRole('button', { name: 'Show password' }));

    const input = screen.getByLabelText('Password');
    expect(input).toHaveAttribute('type', 'text');
    const toggle = screen.getByRole('button', { name: 'Hide password' });
    expect(toggle).toHaveAttribute('aria-pressed', 'true');

    fireEvent.click(toggle);
    expect(input).toHaveAttribute('type', 'password');
    expect(screen.getByRole('button', { name: 'Show password' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('propagates typing through onChange', () => {
    render(<Harness initial="" />);

    const input = screen.getByLabelText('Password');
    fireEvent.change(input, { target: { value: 'correct-horse' } });
    expect(input).toHaveValue('correct-horse');
  });
});
