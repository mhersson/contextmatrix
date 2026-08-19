/**
 * Mirrors `IsTerminalState` in `internal/board/project.go`. Stalled is
 * deliberately not terminal - it is system-managed and recoverable.
 */
export function isTerminalState(state: string): boolean {
  return state === 'done' || state === 'not_planned';
}
