import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import { ModelContextMenu } from './ModelContextMenu';

function renderMenu(overrides: Partial<Parameters<typeof ModelContextMenu>[0]> = {}) {
  const onBlacklist = vi.fn();
  const onDelist = vi.fn();
  const onClose = vi.fn();
  const utils = render(
    <ModelContextMenu slug="z-ai/glm-5.2" x={40} y={60} blacklisted={false} onBlacklist={onBlacklist} onDelist={onDelist} onClose={onClose} {...overrides} />,
  );
  return { onBlacklist, onDelist, onClose, ...utils };
}

describe('ModelContextMenu', () => {
  it('names the model and offers to blacklist it when it is not blacklisted', () => {
    const { onBlacklist, onDelist, onClose } = renderMenu();

    const menu = screen.getByRole('menu', { name: 'z-ai/glm-5.2' });
    expect(menu).toHaveTextContent('z-ai/glm-5.2');
    const item = screen.getByRole('menuitem', { name: 'Add to blacklist' });
    expect(item).toHaveFocus();

    fireEvent.click(item);

    expect(onBlacklist).toHaveBeenCalledWith('z-ai/glm-5.2');
    expect(onDelist).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('offers to delist a blacklisted model instead', () => {
    const { onBlacklist, onDelist, onClose } = renderMenu({ blacklisted: true });

    expect(screen.queryByRole('menuitem', { name: 'Add to blacklist' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Remove from blacklist' }));

    expect(onDelist).toHaveBeenCalledWith('z-ai/glm-5.2');
    expect(onBlacklist).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('opens at the pointer', () => {
    renderMenu({ x: 123, y: 456 });

    const menu = screen.getByRole('menu');
    expect(menu).toHaveStyle({ left: '123px', top: '456px' });
  });

  it('closes on Escape and on a click outside, without acting', () => {
    const { onBlacklist, onClose } = renderMenu();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.mouseDown(document.body);
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onBlacklist).not.toHaveBeenCalled();
  });
});
