import { useEffect, useState } from 'react';
import { api } from '../api/client';
import { safeGetString, safeSetString } from '../utils/safeStorage';

const STORAGE_KEY = 'contextmatrix-agent-id';

function generateAgentId(): string {
  // 8 hex chars from the browser's CSPRNG; 32 bits is plenty to distinguish
  // browsers on a single CM instance. Falls back to Math.random() in any
  // environment without crypto.getRandomValues (older test environments).
  let suffix: string;

  try {
    const buf = new Uint8Array(4);
    crypto.getRandomValues(buf);
    suffix = Array.from(buf, (b) => b.toString(16).padStart(2, '0')).join('');
  } catch {
    suffix = Math.random().toString(16).slice(2, 10).padStart(8, '0');
  }

  return `human:web-${suffix}`;
}

export function useAgentId(enabled: boolean) {
  // When storage throws, fall back to a per-session in-memory id - the only
  // loss is id continuity across page reloads; the CSRF/UI-origin signal is
  // unaffected.
  const [agentId] = useState<string | null>(() => {
    if (!enabled) return null;

    const existing = safeGetString(STORAGE_KEY);
    if (existing) return existing;

    const fresh = generateAgentId();
    safeSetString(STORAGE_KEY, fresh);
    return fresh;
  });

  useEffect(() => {
    api.setAgentId(agentId);
  }, [agentId]);

  return { agentId };
}
