import { useState, useEffect, useCallback } from 'react';

const STORAGE_KEY = 'contextmatrix-agent-id';

export function useAgentId() {
  const [agentId, setAgentIdState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY);
  });

  useEffect(() => {
    if (agentId) {
      localStorage.setItem(STORAGE_KEY, agentId);
    } else {
      localStorage.removeItem(STORAGE_KEY);
    }
  }, [agentId]);

  const setAgentId = useCallback((id: string) => {
    const formatted = id.startsWith('human:') ? id : `human:${id}`;
    setAgentIdState(formatted);
  }, []);

  const clearAgentId = useCallback(() => {
    setAgentIdState(null);
  }, []);

  const promptForAgentId = useCallback((): string | null => {
    const current = localStorage.getItem(STORAGE_KEY);
    if (current) return current;

    const input = window.prompt('Enter your username for claiming cards:');
    if (!input || !input.trim()) return null;

    const formatted = `human:${input.trim()}`;
    setAgentIdState(formatted);
    return formatted;
  }, []);

  return {
    agentId,
    setAgentId,
    clearAgentId,
    promptForAgentId,
  };
}
