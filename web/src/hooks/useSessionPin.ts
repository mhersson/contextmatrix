import { useCallback } from 'react';
import { api } from '../api/client';
import { settleEach } from '../components/Sessions/useBulkActions';
import { notifyChatSessionsChanged } from './useChatSessions';
import { useToast } from './useToast';

/** Pins or unpins sessions with one PATCH each, then refreshes every session list once. */
export function useSessionPin(): (ids: string[], pinned: boolean) => Promise<void> {
  const { showToast } = useToast();

  return useCallback(
    async (ids: string[], pinned: boolean) => {
      const failed = (await settleEach(ids, (id) => api.updateChat(id, { pinned }), notifyChatSessionsChanged)).length;
      if (failed > 0) {
        const verb = pinned ? 'pin' : 'unpin';
        showToast(`Could not ${verb} ${failed} session${failed === 1 ? '' : 's'}`, 'error');
      }
    },
    [showToast],
  );
}
