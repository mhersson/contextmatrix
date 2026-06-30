import { useId } from 'react';
import { useOpenRouterModels } from '../../hooks/useOpenRouterModels';
import { useTheme } from '../../hooks/useTheme';
import { formatTokens } from '../../utils/chatModels';
import type { ChatModel } from '../../types';

interface ChatModelPickerProps {
  /** Which picker to render — driven by GET /api/chats/models `source`. */
  source: 'config' | 'openrouter' | 'endpoint';
  /** Current model value (config: allowlist id; openrouter: OpenRouter slug). */
  model: string;
  /** Server default, used to mark the default option in config mode. */
  defaultModel: string;
  /** Config-mode allowlist (empty in openrouter mode). */
  models: ChatModel[];
  onChange: (value: string) => void;
}

/**
 * Model selector for the New Chat dialog. Mirrors the card "pin model"
 * autocomplete when the dedicated chat backend (contextmatrix-chat, OpenRouter)
 * serves chat:
 *  - 'config' (runner serves chat): a <select> over the chat.models allowlist.
 *    Renders nothing when the allowlist is empty.
 *  - 'openrouter': a free-text input with live OpenRouter-catalog autocomplete
 *    plus a favorites chip row (operator-configured slugs from app config).
 *    Always renders, even though the config `models` list is empty.
 *  - 'endpoint': server-provided model list from the configured OpenAI-
 *    compatible endpoint; rendered as a <select>, identical to 'config'.
 */
export function ChatModelPicker({
  source,
  model,
  defaultModel,
  models,
  onChange,
}: ChatModelPickerProps) {
  const listId = useId();
  const fieldId = `${listId}-field`;
  const openRouter = source === 'openrouter';
  // Defer the ~400KB OpenRouter catalog fetch to openrouter mode only.
  const orSlugs = useOpenRouterModels(openRouter);
  const { favorites: favsByTier } = useTheme();
  // Flatten per-tier favorite slugs into one de-duped chip list (same pattern as
  // AutomationTab). Only relevant in openrouter mode.
  const favorites =
    openRouter && favsByTier ? [...new Set(Object.values(favsByTier).flat())] : [];

  const label = (
    <label htmlFor={fieldId} className="block text-xs mb-1" style={{ color: 'var(--grey2)' }}>
      Model
    </label>
  );

  if (!openRouter) {
    // Native allowlist via the runner — hide entirely when none configured.
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
              {m.id === defaultModel ? ' — default' : ''}
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
      <input
        id={fieldId}
        type="text"
        list={listId}
        value={model}
        onChange={(e) => onChange(e.target.value)}
        className="bf-input w-full mb-4 font-mono"
        placeholder="OpenRouter slug, e.g. anthropic/claude-sonnet-4"
      />
      <datalist id={listId}>
        {orSlugs.map((slug) => (
          <option key={slug} value={slug} />
        ))}
      </datalist>
    </>
  );
}
