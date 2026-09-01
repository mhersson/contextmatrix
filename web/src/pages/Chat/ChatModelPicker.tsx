import { useId } from 'react';
import { BlacklistedChip } from '../../components/BlacklistedChip';
import { ModelCombobox } from '../../components/ModelCombobox';
import { useTheme } from '../../hooks/useTheme';
import { formatTokens } from '../../lib/format';
import type { ChatModel } from '../../types';

interface ChatModelPickerProps {
  /** Which picker to render - driven by GET /api/chats/models `source`. */
  source: 'openrouter' | 'endpoint';
  /** Current model value (endpoint: server model id; openrouter: OpenRouter slug). */
  model: string;
  /** Server default, used to mark the default option in endpoint mode. */
  defaultModel: string;
  /** Picker list: the endpoint model list, or the vendor-screened catalog in openrouter mode. */
  models: ChatModel[];
  onChange: (value: string) => void;
}

/**
 * Model selector for the New Chat dialog. Mirrors the card "pin model"
 * autocomplete when the dedicated chat backend (contextmatrix-chat, OpenRouter)
 * serves chat:
 *  - 'openrouter': a strict combobox (`ModelCombobox`) over the server-
 *    provided, vendor-screened model list, plus a favorites chip row
 *    (operator-configured slugs from app config). Degrades to a free-text
 *    input when the server list is empty (catalog-unavailable path). Always
 *    renders, even though the `models` list is empty. A blacklisted entry
 *    (the chip) stays selectable - pinning it is an informed override.
 *  - 'endpoint': server-provided model list from the configured OpenAI-
 *    compatible endpoint; rendered as a <select>, with blacklisted entries
 *    marked in the option text. Renders nothing when the list is empty -
 *    new chats then use the server default.
 */
export function ChatModelPicker({
  source,
  model,
  defaultModel,
  models,
  onChange,
}: ChatModelPickerProps) {
  const fieldId = useId();
  const openRouter = source === 'openrouter';
  const { favorites: favsByTier } = useTheme();
  // Flatten per-tier favorite slugs into one de-duped chip list (same pattern as
  // AutomationTab). Only relevant in openrouter mode.
  const favorites =
    openRouter && favsByTier ? [...new Set(Object.values(favsByTier).flat())] : [];
  // The committed slug is marked by the BlacklistedChip riding next to the
  // combobox (ModelPinsSection pattern); the flagged set also marks matching
  // entries in the dropdown options before anything is committed.
  const blacklistedSlugs = models.filter((m) => m.blacklisted).map((m) => m.id);
  // The combobox shows only the committed slug, so the shared blacklisted chip
  // rides next to it (ModelPinsSection pattern) when that slug is flagged.
  const current = models.find((m) => m.id === model);

  const label = (
    <label htmlFor={fieldId} className="block text-xs mb-1" style={{ color: 'var(--grey2)' }}>
      Model
    </label>
  );

  if (!openRouter) {
    // Endpoint model list - hide entirely when none configured.
    if (models.length === 0) return null;
    return (
      <>
        {label}
        <select
          id={fieldId}
          value={model}
          onChange={(e) => onChange(e.target.value)}
          className="bf-input w-full mb-4"
        >
          {models.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label} ({formatTokens(m.max_tokens)} context)
              {m.blacklisted ? ' - blacklisted (reported incapable; a pin overrides)' : ''}
              {m.id === defaultModel ? ' - default' : ''}
            </option>
          ))}
        </select>
      </>
    );
  }

  return (
    <>
      {label}
      {favorites.length > 0 && (
        <div className="flex flex-wrap gap-1 mb-2">
          {favorites.map((slug) => (
            <button
              key={slug}
              type="button"
              onClick={() => onChange(slug)}
              title={`Use ${slug}`}
              style={{
                background: 'color-mix(in oklab, var(--bg-blue) 70%, transparent)',
                color: 'var(--aqua)',
                border: '1px solid color-mix(in oklab, var(--aqua) 30%, transparent)',
                borderRadius: '4px',
                padding: '1px 7px',
                fontFamily: 'var(--font-mono)',
                fontSize: '10.5px',
                cursor: 'pointer',
                whiteSpace: 'nowrap',
                lineHeight: '1.6',
              }}
            >
              {slug}
            </button>
          ))}
        </div>
      )}
      <div className="mb-4">
        <div className="flex items-center gap-2 min-w-0">
          <ModelCombobox
            id={fieldId}
            value={model}
            onChange={onChange}
            options={models.map((m) => m.id)}
            flaggedSlugs={blacklistedSlugs}
            placeholder="OpenRouter slug, e.g. anthropic/claude-sonnet-4"
            ariaLabel="Model"
            className="bf-input w-full font-mono"
          />
          {current && current.blacklisted && <BlacklistedChip />}
        </div>
      </div>
    </>
  );
}
