import { lazy, Suspense, useState } from 'react';
import { api } from '../../api/client';
import { useTheme } from '../../hooks/useTheme';
import type { KnowledgeDocResponse } from '../../types';
import { KnowledgeDocEditor } from './KnowledgeDocEditor';

const MarkdownPreview = lazy(() => import('@uiw/react-markdown-preview'));

interface ViewerProps {
  project: string;
  repo: string;
  doc: string;
  response: KnowledgeDocResponse;
  onSaved: () => Promise<void>;
  onDirtyChange?: (dirty: boolean) => void;
}

export function KnowledgeDocViewer({ project, repo, doc, response, onSaved, onDirtyChange }: ViewerProps) {
  const [editing, setEditing] = useState(false);
  const { theme } = useTheme();

  if (editing) {
    return (
      <KnowledgeDocEditor
        initialContent={response.content}
        onCancel={() => {
          onDirtyChange?.(false);
          setEditing(false);
        }}
        onSave={async (content) => {
          await api.putKnowledgeDoc(project, repo, doc, content);
          await onSaved();
          onDirtyChange?.(false);
          setEditing(false);
        }}
        onDirtyChange={onDirtyChange}
      />
    );
  }

  return (
    <div className="p-6">
      <div className="flex justify-between items-center mb-4">
        <h1 className="text-xl font-semibold" style={{ color: 'var(--fg)' }}>
          {repo} / {doc}
        </h1>
        <button
          type="button"
          onClick={() => setEditing(true)}
          className="px-3 py-1 text-sm rounded"
          style={{
            border: '1px solid var(--bg3)',
            color: 'var(--fg)',
            backgroundColor: 'transparent',
          }}
        >
          Edit
        </button>
      </div>
      {response.meta.human_edited && (
        <div
          role="status"
          aria-live="polite"
          className="mb-4 px-3 py-2 text-sm rounded"
          style={{ backgroundColor: 'var(--yellow)', color: 'var(--bg0)' }}
        >
          This doc has been hand-edited. Refresh will prompt before overwriting.
        </div>
      )}
      <section data-color-mode={theme}>
        <Suspense fallback={null}>
          <MarkdownPreview source={response.content} skipHtml className="bf-markdown" />
        </Suspense>
      </section>
    </div>
  );
}
