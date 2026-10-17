import type { ReactNode } from 'react';
import type { LogEntry } from '../../types';
import { BlueprintPane } from './BlueprintPane';
import { useBlueprintView, type FrameSource } from './useBlueprintView';

interface BlueprintSessionProps {
  chatId: string;
  /** The session's chat stream, which carries the architect's live frames. */
  frames: FrameSource;
  /** Counts the chat stream's opens; a reopen refetches the view. */
  connection: number;
  logs: readonly LogEntry[];
  promptsLive: boolean;
  onReply: (text: string) => Promise<void>;
  phone: boolean;
  /** The transcript. */
  children: ReactNode;
}

/** An architect session's pane: the transcript with the blueprint scale bar and sheet. */
export function BlueprintSession({ chatId, frames, connection, logs, ...rest }: BlueprintSessionProps) {
  const { state, retry } = useBlueprintView(chatId, frames, connection, logs);
  return <BlueprintPane chatId={chatId} state={state} onRetry={retry} logs={logs} {...rest} />;
}
