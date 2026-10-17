import { describe, it, expect } from 'vitest';
import { useRef } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { useFocusTrap } from './useFocusTrap';

function Trap() {
  const ref = useRef<HTMLDivElement>(null);
  useFocusTrap(ref, true);
  return (
    <div ref={ref}>
      <button tabIndex={-1}>out of order</button>
      <button>first</button>
      <button>last</button>
    </div>
  );
}

describe('useFocusTrap', () => {
  it('wraps Shift+Tab from the first tabbable element, skipping tabindex=-1 buttons', () => {
    render(<Trap />);
    const first = screen.getByRole('button', { name: 'first' });
    first.focus();
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
    expect(screen.getByRole('button', { name: 'last' })).toHaveFocus();
  });
});
