import type { PlaybookSegment } from '../../types';
import { segmentColor } from './playbookUtils';

interface SegmentedProgressProps {
  segments: PlaybookSegment[];
  className?: string;
}

export function SegmentedProgress({ segments, className }: SegmentedProgressProps) {
  const complete = segments.filter((s) => s === 'complete').length;
  return (
    <div
      className={`flex gap-0.5 ${className ?? ''}`}
      role="img"
      aria-label={`${complete} of ${segments.length} complete`}
    >
      {segments.map((seg, i) => (
        <span
          key={i}
          className="h-2 flex-1 rounded-[2px]"
          style={{ backgroundColor: segmentColor(seg) }}
        />
      ))}
    </div>
  );
}
