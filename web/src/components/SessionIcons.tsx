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

/** Push pin: a pinned session when filled. */
export function PinIcon({ size = 14, filled = false, label }: IconProps & { filled?: boolean }) {
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
      <path d="M12 17v5" />
      <path
        d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V17h14v-1.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"
        fill={filled ? 'currentColor' : 'none'}
      />
    </svg>
  );
}
