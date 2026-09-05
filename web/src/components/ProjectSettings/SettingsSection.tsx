import { useId, type ReactNode } from 'react';

interface SettingsSectionProps {
  title: string;
  /** One grey line under the eyebrow explaining what the group controls. */
  lead?: ReactNode;
  children: ReactNode;
}

/** Hairline-separated group inside a settings tab: mono eyebrow, optional
 *  lead, then the fields. Mirrors the card panel's `.bf-aside-section`. */
export function SettingsSection({ title, lead, children }: SettingsSectionProps) {
  const headingId = useId();
  return (
    <section className="ps-section" aria-labelledby={headingId}>
      <h3 id={headingId} className="section-eyebrow">{title}</h3>
      {lead && <p className="ps-lead">{lead}</p>}
      {children}
    </section>
  );
}
