export function BoardSkeleton() {
  return (
    <div className="flex flex-col h-full">
      {/* Header skeleton */}
      <div className="px-4 py-3 border-b border-[var(--bg3)] flex items-center justify-between">
        <div>
          <div className="h-5 w-32 bg-[var(--bg2)] rounded animate-pulse" />
          <div className="h-3 w-16 bg-[var(--bg2)] rounded animate-pulse mt-1" />
        </div>
        <div className="h-8 w-24 bg-[var(--bg2)] rounded animate-pulse" />
      </div>

      {/* Columns skeleton */}
      <div className="flex-1 overflow-hidden">
        <div className="flex gap-4 p-4 h-full">
          {[...Array(5)].map((_, i) => (
            <div
              key={i}
              className="flex-shrink-0 flex flex-col bg-[var(--bg0)] rounded-lg border border-[var(--bg3)]"
              style={{ width: 'var(--col-width, 280px)', minWidth: 'var(--col-width, 280px)' }}
            >
              {/* Column header */}
              <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--bg3)]">
                <div className="h-4 w-20 bg-[var(--bg2)] rounded animate-pulse" />
                <div className="h-4 w-6 bg-[var(--bg2)] rounded animate-pulse" />
              </div>

              {/* Card placeholders */}
              <div className="p-2 space-y-2">
                {[72, 56, 88].map((h, j) => (
                  <div
                    key={j}
                    className="rounded bg-[var(--bg1)] animate-pulse"
                    style={{ height: h }}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
