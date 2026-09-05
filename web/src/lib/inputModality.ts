/**
 * Tracks whether the user's last input was a pointer or the keyboard, the
 * same signal browsers use for :focus-visible. Lets focus handlers tell a
 * Tab-conferred focus from one a click confers or one that focus
 * restoration hands back after a mouse interaction (jsdom's :focus-visible
 * heuristics are not reliable enough to query directly).
 */
let lastInput: 'keyboard' | 'pointer' = 'keyboard';
let listening = false;

function listen() {
  if (listening || typeof document === 'undefined') return;
  listening = true;
  const pointer = () => { lastInput = 'pointer'; };
  document.addEventListener('pointerdown', pointer, true);
  document.addEventListener('mousedown', pointer, true);
  document.addEventListener('keydown', (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    lastInput = 'keyboard';
  }, true);
}

/** True when the most recent user input came from the keyboard. */
export function lastInputWasKeyboard(): boolean {
  listen();
  return lastInput === 'keyboard';
}

listen();
