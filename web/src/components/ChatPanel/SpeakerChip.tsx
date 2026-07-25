import { memo } from 'react';
import { idColor } from '../../utils/colorHash';

/**
 * Speaker attribution for mob session discussion messages. The speaker hue is a
 * deterministic bucket over the shared 5-accent palette (idColor hashes the
 * author name onto CSS custom properties), so the same author always gets
 * the same color and no hex ever appears here.
 *
 * When `model` is present and non-empty, a second pill is rendered on the
 * same line (flex row) beside the speaker pill. The model pill uses the
 * `--purple` accent - the same semantic token the Automation tab uses for
 * mob-phase chips (background `--bg-purple`, text `--purple`) - so it reads
 * as a consistent, mob-related accent regardless of the author's own color.
 * Long model slugs (e.g. `z-ai/glm-5.2`) wrap naturally; truncation, if
 * needed on narrow panes, is a follow-up.
 */
function SpeakerChipImpl({ author, model }: { author: string; model?: string }) {
  const accent = idColor(author);
  const showModel = typeof model === 'string' && model.length > 0;
  return (
    <div className="flex flex-row items-center gap-1.5" style={{ marginBottom: '2px' }}>
      <span
        className="chip-pill font-mono"
        data-testid="speaker-chip"
        style={{
          backgroundColor: `color-mix(in srgb, ${accent} 16%, transparent)`,
          color: accent,
          fontSize: '10px',
        }}
        title={`Speaker: ${author}`}
      >
        {author}
      </span>
      {showModel && (
        <span
          className="chip-pill font-mono"
          data-testid="model-chip"
          style={{
            backgroundColor: 'var(--bg-purple)',
            color: 'var(--purple)',
            fontSize: '10px',
          }}
          title={`Model: ${model}`}
        >
          {model}
        </span>
      )}
    </div>
  );
}

export const SpeakerChip = memo(SpeakerChipImpl);
