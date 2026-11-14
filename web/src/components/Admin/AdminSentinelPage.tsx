import { useEffect, useId, useState } from 'react';
import { api, describeApiError } from '../../api/client';
import { useSentinel } from '../../hooks/useSentinel';
import { formatStamp } from '../../lib/cardTiming';
import type { SentinelInfo, SentinelSettings } from '../../types';

type Draft = Omit<SentinelSettings, 'headroom'> & { headroomPercent: number };

const toDraft = (s: SentinelSettings): Draft => ({
  ...s,
  headroomPercent: Math.round(s.headroom * 10000) / 100,
});

const toSettings = ({ headroomPercent, ...rest }: Draft): SentinelSettings => ({
  ...rest,
  headroom: headroomPercent / 100,
});

function Switch({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <div className="flex items-center gap-3">
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={label}
        disabled={disabled}
        className={`tl-switch${checked ? ' on' : ''}`}
        style={{ opacity: disabled ? 0.5 : 1 }}
        onClick={() => onChange(!checked)}
      >
        <span className="tl-switch-knob" aria-hidden="true" />
      </button>
      <div>
        <div className="text-sm" style={{ color: 'var(--fg)' }}>
          {label}
        </div>
        <div className="text-xs" style={{ color: 'var(--grey1)' }}>
          {hint}
        </div>
      </div>
    </div>
  );
}

function NumberField({
  label,
  hint,
  value,
  step,
  onChange,
}: {
  label: string;
  hint: string;
  value: number;
  step: number;
  onChange: (v: number) => void;
}) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm" style={{ color: 'var(--fg)' }}>
        {label}
      </label>
      <input
        id={id}
        type="number"
        step={step}
        value={Number.isNaN(value) ? '' : value}
        onChange={(e) => onChange(e.target.value === '' ? NaN : Number(e.target.value))}
        className="bf-input"
        style={{ maxWidth: '12rem' }}
      />
      <div className="text-xs" style={{ color: 'var(--grey1)' }}>
        {hint}
      </div>
    </div>
  );
}

/** Admin-only Sentinel settings: the switches, spend caps and subscription headroom threshold. */
export function AdminSentinelPage() {
  const { refresh } = useSentinel();
  const [info, setInfo] = useState<SentinelInfo | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api
      .getSentinel()
      .then((v) => {
        setInfo(v);
        setDraft(toDraft(v.settings));
      })
      .catch((err) => setError(describeApiError(err, 'Failed to load Sentinel settings.')));
  }, []);

  const save = async () => {
    if (!draft) return;
    if (![draft.daily_cap_usd, draft.investigate_ceiling_usd, draft.headroomPercent].every(Number.isFinite)) {
      setError('Enter a number in every field.');
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const v = await api.putSentinelSettings(toSettings(draft));
      setInfo(v);
      setDraft(toDraft(v.settings));
      refresh();
    } catch (err) {
      setError(describeApiError(err, 'Failed to save Sentinel settings.'));
    } finally {
      setSaving(false);
    }
  };

  const set = (patch: Partial<Draft>) => setDraft((d) => (d ? { ...d, ...patch } : d));

  return (
    <div className="p-6 flex flex-col gap-4 max-w-xl">
      <h1 className="text-lg font-semibold" style={{ color: 'var(--fg)' }}>
        Sentinel
      </h1>

      {draft && (
        <>
          <Switch
            label="Sentinel"
            hint="Watch autonomous runs and act on what stops them. Turning it on starts fresh: runs launched before then are left to you."
            checked={draft.enabled}
            onChange={(enabled) => set({ enabled })}
          />
          <Switch
            label="Healing"
            hint="Diagnose stuck autonomous runs, fix the card and rerun it. Turning it on starts fresh: runs launched before then are left to you."
            checked={draft.healing}
            disabled={!draft.enabled}
            onChange={(healing) => set({ healing })}
          />
          <Switch
            label="Friction analysis"
            hint="Keep and show the friction findings and notes each investigation reports"
            checked={draft.friction}
            disabled={!draft.enabled}
            onChange={(friction) => set({ friction })}
          />
          <NumberField
            label="Daily spend cap (USD)"
            hint="Applies on the endpoint channel to investigations and reruns, counting work still in flight. 0 stops both."
            step={1}
            value={draft.daily_cap_usd}
            onChange={(daily_cap_usd) => set({ daily_cap_usd })}
          />
          <NumberField
            label="Investigation ceiling (USD per investigation)"
            hint="The most one investigation may cost."
            step={0.5}
            value={draft.investigate_ceiling_usd}
            onChange={(investigate_ceiling_usd) => set({ investigate_ceiling_usd })}
          />
          <NumberField
            label="Subscription headroom threshold (percent)"
            hint="On Claude Code, Sentinel waits while a usage window is above this."
            step={1}
            value={draft.headroomPercent}
            onChange={(headroomPercent) => set({ headroomPercent })}
          />
          <div>
            <button type="button" className="bf-btn-primary" disabled={saving} onClick={() => void save()}>
              Save
            </button>
          </div>
        </>
      )}

      {error && (
        <div className="text-sm" role="alert" style={{ color: 'var(--red)' }}>
          {error}
        </div>
      )}

      {info?.updated_by && info.updated_at && (
        <div className="text-xs" style={{ color: 'var(--grey1)' }}>
          Updated by {info.updated_by} at {formatStamp(info.updated_at)}
        </div>
      )}
    </div>
  );
}
