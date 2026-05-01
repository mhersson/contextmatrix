import { useState, useEffect, useCallback } from 'react';
import { api } from '../api/client';

const STORAGE_KEY = 'contextmatrix-agent-id';
const DEFAULT_AGENT_ID = 'human:user';

export function useAgentId() {
  const [agentId, setAgentIdState] = useState<string>(() => {
    return localStorage.getItem(STORAGE_KEY) ?? DEFAULT_AGENT_ID;
  });

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, agentId);
    api.setAgentId(agentId);
  }, [agentId]);

  // setAgentId syncs api.agentId synchronously in addition to queuing
  // the state update. The useEffect above is the safety net for the
  // initial-mount case, but it fires only after React commits — too
  // late for code paths that set the agent id and immediately call an
  // authenticated endpoint in the same tick.
  const setAgentId = useCallback((id: string) => {
    const formatted = id.startsWith('human:') ? id : `human:${id}`;
    setAgentIdState(formatted);
    api.setAgentId(formatted);
  }, []);

  return {
    agentId,
    setAgentId,
  };
}
