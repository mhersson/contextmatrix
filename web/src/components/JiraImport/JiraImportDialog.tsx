import { useEffect } from 'react';
import { api } from '../../api/client';

export interface JiraImportDialogProps {
  epicKey: string;
  description: string;
  boardsRepo?: string;
  onClose: () => void;
}

export function JiraImportDialog(props: JiraImportDialogProps) {
  const { epicKey } = props;
  useEffect(() => {
    api.previewJiraEpic(epicKey).catch(() => {});
  }, [epicKey]);
  return <div role="dialog" aria-label={`Import Jira epic ${epicKey}`} />;
}
