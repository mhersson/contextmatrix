import { useEffect, useState } from 'react';
import { api } from '../../api/client';
import { errorWithDetails } from '../../lib/errors';
import type { JiraEpicPreview } from '../../types';
import { initialForm, type ImportForm } from './jiraImportModel';

export interface EpicPreview {
  preview: JiraEpicPreview | null;
  form: ImportForm | null;
  setForm: (form: ImportForm) => void;
  error: string | null;
}

/** Loads an epic's import preview and seeds the selection form from it. */
export function useJiraEpicPreview(epicKey: string): EpicPreview {
  const [preview, setPreview] = useState<JiraEpicPreview | null>(null);
  const [form, setForm] = useState<ImportForm | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    api
      .previewJiraEpic(epicKey)
      .then((p) => {
        if (cancelled) return;
        setPreview(p);
        setForm(initialForm(p));
      })
      .catch((err) => {
        if (!cancelled) setError(errorWithDetails(err, 'Could not load the epic from Jira'));
      });
    return () => {
      cancelled = true;
    };
  }, [epicKey]);

  return { preview, form, setForm, error };
}
