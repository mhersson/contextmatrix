import { useCallback } from 'react';
import { api } from '../../api/client';
import { usePlaybooks } from '../../hooks/usePlaybooks';
import { useToast } from '../../hooks/useToast';
import { settleEach } from '../Sessions/useBulkActions';

/** Pins or unpins playbooks for the viewer, one request each, then refreshes the playbook list once. */
export function usePlaybookPin(): (ids: string[], pinned: boolean) => Promise<void> {
  const { showToast } = useToast();
  const { refresh } = usePlaybooks();

  return useCallback(
    async (ids: string[], pinned: boolean) => {
      const failed = (await settleEach(ids, (id) => api.pinPlaybook(id, pinned), refresh)).length;
      if (failed > 0) {
        const verb = pinned ? 'pin' : 'unpin';
        showToast(`Could not ${verb} ${failed} playbook${failed === 1 ? '' : 's'}`, 'error');
      }
    },
    [showToast, refresh],
  );
}
