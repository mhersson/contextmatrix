interface IconProps {
  size?: number;
  /** Names the icon for assistive tech; without it the icon is decorative. */
  label?: string;
}

function a11y(label?: string) {
  return label ? { role: 'img', 'aria-label': label } : { 'aria-hidden': true };
}

/** Drafting compass: a blueprint session. */
export function BlueprintIcon({ size = 14, label }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...a11y(label)}
    >
      <path d="M12 2v2" />
      <circle cx="12" cy="6" r="2" />
      <path d="M11.15 7.81 5 21" />
      <path d="M12.85 7.81 19 21" />
      <path d="M7.8 15a9.93 9.93 0 0 0 8.4 0" />
    </svg>
  );
}

/** Speech bubble: a plain chat session. */
export function ChatIcon({ size = 14, label }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.3}
      strokeLinejoin="round"
      {...a11y(label)}
    >
      <path d="M2.5 3.5h11v7h-6l-3 2.5v-2.5h-2z" />
    </svg>
  );
}
