import { Link } from 'react-router-dom';

export function NotFound() {
  return (
    <div
      className="flex flex-col items-center justify-center h-full gap-4"
      style={{ backgroundColor: 'var(--bg0)', color: 'var(--fg)' }}
    >
      <div
        className="text-8xl font-bold"
        style={{ color: 'var(--red)' }}
        aria-hidden="true"
      >
        404
      </div>
      <h1 className="text-2xl font-semibold" style={{ color: 'var(--fg)' }}>
        Page not found
      </h1>
      <p style={{ color: 'var(--grey1)' }}>
        The page you're looking for doesn't exist or has been moved.
      </p>
      <Link
        to="/"
        className="mt-2 px-4 py-2 rounded text-sm font-medium"
        style={{
          backgroundColor: 'var(--bg2)',
          color: 'var(--aqua)',
          border: '1px solid var(--bg3)',
        }}
      >
        Go home
      </Link>
    </div>
  );
}
