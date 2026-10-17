import type { BlueprintView } from '../../types';
import type { HeldDraft } from './draft';

export type BlueprintState =
  | { status: 'loading' }
  /** No blueprint view: an older architect, or none configured; the pane is a plain transcript. */
  | { status: 'absent' }
  | { status: 'unreachable' }
  | { status: 'ready'; view: BlueprintView; draft: HeldDraft | null };
