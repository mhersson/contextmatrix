import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { TechnicalDetail } from './TechnicalDetail';

describe('TechnicalDetail', () => {
  it('draws no fold for a whitespace-only detail', () => {
    const { container } = render(<TechnicalDetail detail={'  \n\t '} />);
    expect(container.querySelector('details')).toBeNull();
  });

  it('draws no fold for a missing detail', () => {
    const { container } = render(<TechnicalDetail />);
    expect(container.querySelector('details')).toBeNull();
  });

  it('folds a real detail closed with its text inside', () => {
    const { container } = render(<TechnicalDetail detail="uses a hosted database" />);
    const fold = container.querySelector('details');
    expect(fold).not.toBeNull();
    expect(fold!.open).toBe(false);
    expect(fold!.textContent).toContain('uses a hosted database');
  });

  it('shows what render returns instead of the raw text', () => {
    const { container } = render(<TechnicalDetail detail="raw" render={(d) => <em data-testid="r">{d.toUpperCase()}</em>} />);
    expect(container.querySelector('details em')?.textContent).toBe('RAW');
  });
});
