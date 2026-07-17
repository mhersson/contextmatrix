import { describe, it, expect } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { VerifySection } from './VerifySection';
import type { VerifyConfig } from './VerifySection';

// Harness mirrors ProjectSettings: it owns the VerifyConfig state and feeds it
// back into VerifySection on every onChange, so a controlled-input revert bug
// reproduces exactly as it would in the real settings form.
function Harness({ initial = {} }: { initial?: VerifyConfig }) {
  const [value, setValue] = useState<VerifyConfig>(initial);
  return (
    <>
      <VerifySection value={value} onChange={setValue} inputStyle={{}} />
      <output data-testid="env-json">{JSON.stringify(value.env ?? null)}</output>
    </>
  );
}

describe('VerifySection env input', () => {
  it('preserves separators while typing multiple env names incrementally', () => {
    render(<Harness />);

    const envInput = screen.getByLabelText(/passthrough env names/i) as HTMLInputElement;

    // Simulate typing "JAVA_HOME, CGO_ENABLED" one intermediate string at a
    // time - each fireEvent.change is what a keystroke produces.
    const keystrokes = ['J', 'JAVA_HOME', 'JAVA_HOME,', 'JAVA_HOME, ', 'JAVA_HOME, C', 'JAVA_HOME, CGO_ENABLED'];
    for (const s of keystrokes) {
      fireEvent.change(envInput, { target: { value: s } });
      // The DOM must retain exactly what was typed - no mid-word revert.
      expect(envInput.value).toBe(s);
    }

    // The parent received both parsed names.
    expect(screen.getByTestId('env-json').textContent).toBe(
      JSON.stringify(['JAVA_HOME', 'CGO_ENABLED']),
    );
  });

  it('re-syncs the raw text when the loaded value changes externally', () => {
    function ExternalHarness() {
      const [value, setValue] = useState<VerifyConfig>({ env: ['A'] });
      return (
        <>
          <VerifySection value={value} onChange={setValue} inputStyle={{}} />
          <button onClick={() => setValue({ env: ['X', 'Y'] })}>load</button>
        </>
      );
    }
    render(<ExternalHarness />);

    const envInput = screen.getByLabelText(/passthrough env names/i) as HTMLInputElement;
    expect(envInput.value).toBe('A');

    fireEvent.click(screen.getByRole('button', { name: /load/i }));
    expect(envInput.value).toBe('X, Y');
  });
});
