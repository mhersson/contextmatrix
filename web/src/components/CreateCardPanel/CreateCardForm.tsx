import { useState, useEffect, useRef, useCallback } from 'react';
import MDEditor from '@uiw/react-md-editor';
import { useTheme } from '../../hooks/useTheme';
import { ParentSearch } from './ParentSearch';
import type { Card, ProjectConfig } from '../../types';

interface CreateCardFormProps {
  title: string;
  setTitle: (v: string) => void;
  type: string;
  setType: (v: string) => void;
  priority: string;
  setPriority: (v: string) => void;
  labels: string[];
  setLabels: (v: string[]) => void;
  parent: string;
  setParent: (v: string) => void;
  body: string;
  setBody: (v: string) => void;
  config: ProjectConfig;
  cards: Card[];
  bodyDirty: boolean;
  setBodyDirty: (v: boolean) => void;
}

export function CreateCardForm({
  title, setTitle, type, setType, priority, setPriority,
  labels, setLabels, parent, setParent, body, setBody,
  config, cards, bodyDirty, setBodyDirty,
}: CreateCardFormProps) {
  const { theme } = useTheme();
  const titleRef = useRef<HTMLInputElement>(null);
  const [labelInput, setLabelInput] = useState('');

  useEffect(() => {
    titleRef.current?.focus();
  }, []);

  const handleTypeChange = useCallback((newType: string) => {
    const template = config.templates?.[newType];
    if (template) {
      if (bodyDirty) {
        if (window.confirm(`Load template for "${newType}"? This will replace the current body.`)) {
          setBody(template);
          setBodyDirty(false);
        }
      } else {
        setBody(template);
      }
    }
    setType(newType);
  }, [config.templates, bodyDirty, setBody, setBodyDirty, setType]);

  const addLabel = useCallback(() => {
    const trimmed = labelInput.trim();
    if (trimmed && !labels.includes(trimmed)) {
      setLabels([...labels, trimmed]);
      setLabelInput('');
    }
  }, [labelInput, labels, setLabels]);

  const removeLabel = useCallback((label: string) => {
    setLabels(labels.filter((l) => l !== label));
  }, [labels, setLabels]);

  return (
    <div className="space-y-4">
      {/* State hint */}
      <div className="text-xs text-[var(--grey1)] flex items-center gap-2">
        <span>Cards are created in</span>
        <span className="px-1.5 py-0.5 rounded bg-[var(--bg2)] text-[var(--grey2)] font-medium">
          {config.states[0]}
        </span>
      </div>

      {/* Title */}
      <div>
        <label className="block text-xs text-[var(--grey1)] mb-1">Title *</label>
        <input
          ref={titleRef}
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Card title..."
          className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
        />
      </div>

      {/* Type + Priority */}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Type</label>
          <select
            value={type}
            onChange={(e) => handleTypeChange(e.target.value)}
            className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          >
            {config.types.map((t) => (
              <option key={t} value={t}>{t}</option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-xs text-[var(--grey1)] mb-1">Priority</label>
          <select
            value={priority}
            onChange={(e) => setPriority(e.target.value)}
            className="w-full px-3 py-2 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          >
            {config.priorities.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </select>
        </div>
      </div>

      {/* Labels */}
      <div>
        <label className="block text-xs text-[var(--grey1)] mb-1">Labels</label>
        <div className="flex flex-wrap gap-2 mb-2">
          {labels.map((label) => (
            <span
              key={label}
              className="inline-flex items-center gap-1 text-xs px-2 py-1 rounded bg-[var(--bg-purple)] text-[var(--purple)]"
            >
              {label}
              <button onClick={() => removeLabel(label)} className="hover:text-[var(--red)] transition-colors">
                x
              </button>
            </span>
          ))}
        </div>
        <div className="flex gap-2">
          <input
            type="text"
            value={labelInput}
            onChange={(e) => setLabelInput(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), addLabel())}
            placeholder="Add label..."
            className="flex-1 px-3 py-1.5 rounded bg-[var(--bg2)] border border-[var(--bg3)] text-sm text-[var(--fg)] focus:outline-none focus:border-[var(--aqua)]"
          />
          <button
            onClick={addLabel}
            className="px-3 py-1.5 rounded bg-[var(--bg3)] text-[var(--grey2)] hover:bg-[var(--bg4)] transition-colors text-sm"
          >
            Add
          </button>
        </div>
      </div>

      {/* Parent */}
      <ParentSearch parent={parent} setParent={setParent} cards={cards} />

      {/* Body */}
      <div data-color-mode={theme}>
        <label className="block text-xs text-[var(--grey1)] mb-1">Description</label>
        <MDEditor
          value={body}
          onChange={(val) => { setBody(val || ''); setBodyDirty(true); }}
          preview="edit"
          height={200}
          visibleDragbar={false}
        />
      </div>
    </div>
  );
}
