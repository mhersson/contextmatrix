import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ChatComposer } from './ChatComposer';
import { BLUEPRINT_MESSAGE_MAX_BYTES, CHAT_MESSAGE_MAX_BYTES } from './chatEntryUtils';

function setup(maxBytes: number, text: string) {
  const onSend = vi.fn();
  render(<ChatComposer onSend={onSend} sendDisabled={false} maxBytes={maxBytes} />);
  const box = screen.getByLabelText('Message') as HTMLTextAreaElement;
  fireEvent.change(box, { target: { value: text } });
  return { onSend, box, send: screen.getByRole('button', { name: 'Send' }) as HTMLButtonElement };
}

describe('ChatComposer byte limit', () => {
  it('sends a 40000-byte message under the blueprint limit', () => {
    const text = 'a'.repeat(40000);
    const { onSend, send } = setup(BLUEPRINT_MESSAGE_MAX_BYTES, text);
    expect(send.disabled).toBe(false);
    fireEvent.click(send);
    expect(onSend).toHaveBeenCalledWith(text);
  });

  it('refuses 8193 bytes under the chat limit without truncating', () => {
    const { onSend, box, send } = setup(CHAT_MESSAGE_MAX_BYTES, 'a'.repeat(8193));
    expect(box.value.length).toBe(8193);
    expect(send.disabled).toBe(true);
    const counter = screen.getByText('8.1 KiB / 8 KiB');
    expect(counter.style.color).toBe('var(--red)');
    fireEvent.keyDown(box, { key: 'Enter' });
    expect(onSend).not.toHaveBeenCalled();
  });

  it('counts multibyte text in bytes', () => {
    const { send } = setup(8192, '€'.repeat(2731));
    expect(send.disabled).toBe(true);
  });
});
