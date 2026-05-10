import { useEffect, useState } from 'react';
import { api, errorMessage } from '../../api/client';
import type { KnowledgeBaseSummary, KnowledgeDocResponse } from '../../types';

interface KnowledgeBaseData {
  summary: KnowledgeBaseSummary | null;
  summaryError: string | null;
  loading: boolean;
  docContent: KnowledgeDocResponse | null;
  docLoading: boolean;
  docError: string | null;
  setDocContent: (doc: KnowledgeDocResponse | null) => void;
  setDocError: (err: string | null) => void;
  setSummary: (s: KnowledgeBaseSummary | null) => void;
  setSummaryError: (err: string | null) => void;
}

export function useKnowledgeBaseData(
  project: string,
  selected: { repo: string; doc: string } | null,
): KnowledgeBaseData {
  const [summary, setSummary] = useState<KnowledgeBaseSummary | null>(null);
  const [summaryError, setSummaryError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [docContent, setDocContent] = useState<KnowledgeDocResponse | null>(null);
  const [docLoading, setDocLoading] = useState(false);
  const [docError, setDocError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const fetchSummary = async () => {
      try {
        const s = await api.getKnowledgeBase(project);
        if (!cancelled) {
          setSummary(s);
          setLoading(false);
        }
      } catch (err) {
        if (!cancelled) {
          setSummaryError(errorMessage(err));
          setSummary(null);
          setLoading(false);
        }
      }
    };
    void fetchSummary();
    return () => {
      cancelled = true;
    };
  }, [project]);

  const selectedRepo = selected?.repo ?? null;
  const selectedDoc = selected?.doc ?? null;
  const docKey = selectedRepo && selectedDoc ? `${selectedRepo}/${selectedDoc}` : null;

  // In-render reset on selection change: clear stale doc state and flip the
  // loading flag synchronously when the input changes. Avoids the
  // setState-in-effect anti-pattern flagged by react-hooks/set-state-in-effect.
  const [prevKey, setPrevKey] = useState<string | null>(null);
  if (docKey !== prevKey) {
    setPrevKey(docKey);
    setDocContent(null);
    setDocError(null);
    setDocLoading(docKey !== null);
  }

  useEffect(() => {
    if (!selectedRepo || !selectedDoc) return;
    let cancelled = false;
    (async () => {
      try {
        const doc = await api.getKnowledgeDoc(project, selectedRepo, selectedDoc);
        if (!cancelled) setDocContent(doc);
      } catch (err) {
        if (!cancelled) setDocError(errorMessage(err));
      } finally {
        if (!cancelled) setDocLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [project, selectedRepo, selectedDoc]);

  return {
    summary,
    summaryError,
    loading,
    docContent,
    docLoading,
    docError,
    setDocContent,
    setDocError,
    setSummary,
    setSummaryError,
  };
}
