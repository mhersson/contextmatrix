import { useEffect, useId, useState } from 'react';
import type { CSSProperties } from 'react';
import { api, isAPIError } from '../../api/client';

export interface WorkerImageSelectProps {
  /** Which backend's node to list images from. */
  backend: 'agent' | 'chat';
  label: string;
  /** Selected image tag, or "" for the backend's configured base_image. */
  value: string;
  onChange: (next: string) => void;
  /** Non-admins in multi mode: render plain text, skip the fetch (403). */
  readOnly: boolean;
  inputStyle: CSSProperties;
  /** Optional caption rendered under the select. */
  hint?: string;
}

const BACKEND_DEFAULT_LABEL = 'Backend default (base_image)';

/**
 * Project-settings picker for a per-backend worker image. Options come from
 * GET /api/backends/{backend}/images — the images actually present on that
 * backend's node, pre-filtered by its image_list_filters. Strict select, no
 * free text: registry refs not on the node are configured by pulling them to
 * the node (or via the REST API, which stays hygiene-validated only).
 *
 * The saved-value "is it on the node" check matches against each image's
 * tags AND digests — operators are encouraged to pin by digest
 * (`repo@sha256:...`), which never appears in `tags`.
 *
 * A saved value missing from the fetched list (by tag or digest) stays
 * selectable with a warning so saving unrelated settings never breaks and
 * nothing is silently substituted. When the list cannot be loaded (backend
 * down or disabled), or while it is still loading, the options reduce to the
 * backend default plus the saved value.
 */
export function WorkerImageSelect({
  backend,
  label,
  value,
  onChange,
  readOnly,
  inputStyle,
  hint,
}: WorkerImageSelectProps) {
  const [tags, setTags] = useState<string[]>([]);
  const [digests, setDigests] = useState<string[]>([]);
  const [loading, setLoading] = useState(!readOnly);
  const [error, setError] = useState<string | null>(null);
  const selectId = useId();

  useEffect(() => {
    if (readOnly) return;
    let cancelled = false;
    api
      .getBackendImages(backend)
      .then((resp) => {
        if (cancelled) return;
        setTags(resp.images.flatMap((img) => img.tags));
        setDigests(resp.images.flatMap((img) => img.digests ?? []));
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(isAPIError(err) ? err.error : 'Failed to load images');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [backend, readOnly]);

  if (readOnly) {
    return (
      <div>
        <div className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
          {label}
        </div>
        <div
          className="px-3 py-2 rounded text-sm"
          style={{ backgroundColor: 'var(--bg1)', color: 'var(--grey2)' }}
        >
          {value || BACKEND_DEFAULT_LABEL}
        </div>
      </div>
    );
  }

  const valueInTags = tags.includes(value);
  const knownRefs = new Set<string>([...tags, ...digests]);
  const isMissing = !loading && !error && value !== '' && !knownRefs.has(value);
  // The controlled select's displayed value must always match an <option>.
  // Cover every case that isn't already a plain tag option and isn't the
  // "genuinely missing" case (which gets the alert-suffixed option below):
  // still loading (unknown yet), the fetch failed, or the value matched via
  // a digest rather than a tag.
  const needsPlainValueOption = value !== '' && !valueInTags && !isMissing;

  return (
    <div>
      <label htmlFor={selectId} className="block text-xs mb-1" style={{ color: 'var(--grey1)' }}>
        {label}
      </label>
      <select
        id={selectId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="w-full px-3 py-2 rounded text-sm border focus:outline-none"
        style={inputStyle}
      >
        <option value="">{BACKEND_DEFAULT_LABEL}</option>
        {tags.map((tag) => (
          <option key={tag} value={tag}>
            {tag}
          </option>
        ))}
        {needsPlainValueOption && <option value={value}>{value}</option>}
        {isMissing && <option value={value}>{`${value} (not on worker node)`}</option>}
      </select>
      {error && (
        <div className="text-xs mt-1" style={{ color: 'var(--grey1)' }}>
          could not load the image list — {error}
        </div>
      )}
      {isMissing && (
        <div className="text-xs mt-1" role="alert" style={{ color: 'var(--red)' }}>
          image not present on the worker node — runs will fail until it is
          rebuilt or a listed image is selected
        </div>
      )}
      {hint && (
        <p className="mt-1 text-xs" style={{ color: 'var(--grey1)' }}>
          {hint}
        </p>
      )}
    </div>
  );
}
