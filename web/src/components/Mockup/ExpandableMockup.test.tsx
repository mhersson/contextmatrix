import { useEffect, useRef, useState } from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ExpandableMockup } from './ExpandableMockup';
import { RENDER_MESSAGE, SETTINGS_MESSAGE } from './frameProtocol';
import { MOCKUP_LABELS } from './labels';

const SRC = '<!doctype html><p>hello</p>';
const declare = (width: string) => `<!doctype html><meta name="viewport" content="width=${width}"><p>hello</p>`;

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

const expand = () => screen.getByRole('button', { name: MOCKUP_LABELS.expand });
const closeX = () => screen.getByRole('button', { name: MOCKUP_LABELS.closeFullSize });
const dialog = () => screen.getByRole('dialog');
const noDialog = () => expect(screen.queryByRole('dialog')).toBeNull();
const overlayFrame = () => dialog().querySelector('iframe')!;
const overlayPane = () => dialog().querySelector<HTMLElement>('.mk-overlay-pane')!;

describe('ExpandableMockup', () => {
  it('opens a modal on the body, outside every container that could clip it', () => {
    render(<ExpandableMockup source={SRC} title="t" />);
    noDialog();
    fireEvent.click(expand());
    expect(dialog()).toHaveAttribute('aria-modal', 'true');
    expect(dialog().parentElement).toBe(document.body);
  });

  it('runs its own sealed frame while the pane frame stays mounted', () => {
    const { container } = render(<ExpandableMockup source={SRC} title="t" />);
    const paneFrame = container.querySelector('iframe')!;
    fireEvent.click(expand());
    expect(overlayFrame()).not.toBe(paneFrame);
    expect(overlayFrame().getAttribute('sandbox')).toBe('allow-scripts');
    expect(container.querySelector('iframe')).toBe(paneFrame);
    fireEvent.click(closeX());
    expect(container.querySelector('iframe')).toBe(paneFrame);
  });

  it('hands the overlay frame the source and the current settings, and later changes', () => {
    const { rerender } = render(<ExpandableMockup source={SRC} settings={{ layout: 'rows' }} title="t" />);
    rerender(<ExpandableMockup source={SRC} settings={{ layout: 'cards' }} title="t" />);
    fireEvent.click(expand());
    const post = vi.spyOn(overlayFrame().contentWindow!, 'postMessage');
    fireEvent.load(overlayFrame());
    expect(post.mock.calls).toEqual([[{ type: RENDER_MESSAGE, source: SRC, settings: { layout: 'cards' } }, '*']]);
    rerender(<ExpandableMockup source={SRC} settings={{ layout: 'grid' }} title="t" />);
    expect(post.mock.calls.at(-1)).toEqual([{ type: SETTINGS_MESSAGE, settings: { layout: 'grid' } }, '*']);
  });

  it('closes on a click outside the mockup', () => {
    render(<ExpandableMockup source={SRC} title="t" />);
    fireEvent.click(expand());
    fireEvent.click(dialog());
    noDialog();
  });

  it('stays open on a click on the mockup', () => {
    render(<ExpandableMockup source={SRC} title="t" />);
    fireEvent.click(expand());
    fireEvent.click(overlayPane());
    fireEvent.click(overlayFrame());
    expect(dialog()).toBeInTheDocument();
  });

  it('closes on the X', () => {
    render(<ExpandableMockup source={SRC} title="t" />);
    fireEvent.click(expand());
    fireEvent.click(closeX());
    noDialog();
  });

  it.each([
    ['a click outside', () => fireEvent.click(dialog())],
    ['the X', () => fireEvent.click(closeX())],
  ])('moves focus into the overlay and back to the expand button after %s', (_name, close) => {
    render(<ExpandableMockup source={SRC} title="t" />);
    fireEvent.click(expand());
    expect(dialog().contains(document.activeElement)).toBe(true);
    close();
    expect(document.activeElement).toBe(expand());
  });

  it('returns focus to the expand button even when opening let another control take the focus first', () => {
    // A chat pane focuses its composer on the first click inside it, and the composer is drawn before the sheet.
    function Composer({ paneFocused }: { paneFocused: boolean }) {
      const input = useRef<HTMLTextAreaElement>(null);
      useEffect(() => {
        if (paneFocused) input.current?.focus();
      }, [paneFocused]);
      return <textarea ref={input} />;
    }
    function Pane() {
      const [paneFocused, setPaneFocused] = useState(false);
      return (
        <div onClick={() => setPaneFocused(true)}>
          <Composer paneFocused={paneFocused} />
          <ExpandableMockup source={SRC} title="t" />
        </div>
      );
    }
    render(<Pane />);
    fireEvent.click(expand());
    expect(dialog().contains(document.activeElement)).toBe(true);
    fireEvent.click(closeX());
    expect(document.activeElement).toBe(expand());
  });

  it.each([
    ['390', '390px'],
    ['1280', '1280px'],
    ['99999', '2560px'],
  ])('caps the overlay pane at a declared width of %s', (width, cap) => {
    render(<ExpandableMockup source={declare(width)} title="t" />);
    fireEvent.click(expand());
    expect(overlayPane().style.maxWidth).toBe(cap);
  });

  it('leaves a mockup that declares no width uncapped', () => {
    render(<ExpandableMockup source={SRC} title="t" />);
    fireEvent.click(expand());
    expect(overlayPane().style.maxWidth).toBe('');
  });
});
