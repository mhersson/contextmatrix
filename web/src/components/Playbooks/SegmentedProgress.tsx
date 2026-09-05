import type { PlaybookSegment } from '../../types';
import { segmentColor } from './playbookUtils';

interface SegmentedProgressProps {
  segments: PlaybookSegment[];
  className?: string;
}

export function SegmentedProgress({ segments, className }: SegmentedProgressProps) {
  const complete = segments.filter((s) => s === 'complete').length;
  const frontier = segments.findIndex((s) => s !== 'complete');
  return (
    <div
      className={`flex gap-0.5 ${className ?? ''}`}
      role="img"
      aria-label={`${complete} of ${segments.length} complete`}
    >
      {segments.map((seg, i) => {
        const isFrontier = i === frontier && seg === 'pending';
        return (
          <span
            key={i}
            className={`h-2 flex-1 rounded-[2px]${isFrontier ? ' pb-seg-frontier' : ''}`}
            style={isFrontier ? undefined : { backgroundColor: segmentColor(seg) }}
          />
        );
      })}
    </div>
  );
}
