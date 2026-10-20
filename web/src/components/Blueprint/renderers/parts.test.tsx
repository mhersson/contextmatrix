import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { codeSpans } from './parts';

const html = (text: string) => {
  const { container } = render(<p>{codeSpans(text)}</p>);
  return container;
};

describe('codeSpans', () => {
  it('renders a backtick pair as code', () => {
    const c = html('a `b` c');
    expect(c.querySelector('code')?.textContent).toBe('b');
    expect(c.textContent).toBe('a b c');
  });

  it.each(['a `b c', 'a `` b'])('leaves %s as plain text', (text) => {
    const c = html(text);
    expect(c.querySelector('code')).toBeNull();
    expect(c.textContent).toBe(text);
  });
});
