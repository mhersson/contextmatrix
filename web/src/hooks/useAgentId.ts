import { useState, useEffect, useCallback } from 'react';
import { api } from '../api/client';

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
    api.setAgentId(agentId);
  }, [agentId]);

  // Each setter syncs api.agentId synchronously in addition to queuing
  // a state update. The useEffect above is the safety net for the
  // initial-mount and external-mutation cases, but it fires only after
  // React commits — too late for code paths that set the agent id and
  // immediately call an authenticated endpoint in the same tick (e.g.
  // promptForAgentId followed by api.promoteCardToAutonomous).
  const setAgentId = useCallback((id: string) => {
    const formatted = id.startsWith('human:') ? id : `human:${id}`;
    setAgentIdState(formatted);
    api.setAgentId(formatted);
  }, []);

  const clearAgentId = useCallback(() => {
    setAgentIdState(null);
    api.setAgentId(null);
  }, []);

  const promptForAgentId = useCallback((): string | null => {
    const current = localStorage.getItem(STORAGE_KEY);
    if (current) {
      api.setAgentId(current);
      return current;
    }

    const input = window.prompt('Enter your username for claiming cards:');
    if (!input || !input.trim()) return null;

    const formatted = `human:${input.trim()}`;
    setAgentIdState(formatted);
    api.setAgentId(formatted);
    return formatted;
  }, []);

  return {
    agentId,
    setAgentId,
    clearAgentId,
    promptForAgentId,
  };
}
