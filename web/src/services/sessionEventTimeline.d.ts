import type { TranscriptEvent } from '@/services/sessionTimeline';

export function appendLiveEvent(
  events: TranscriptEvent[],
  event: TranscriptEvent,
): TranscriptEvent[];
export function prependOlderEvents(
  events: TranscriptEvent[],
  olderEvents: TranscriptEvent[],
): TranscriptEvent[];
export function mergeSnapshotEvents(
  snapshotEvents: TranscriptEvent[],
  currentEvents: TranscriptEvent[],
  bufferedEvents: TranscriptEvent[],
): TranscriptEvent[];
export function shouldReconnectAfterClose(
  acknowledged: boolean,
  accessKeyValid: boolean | undefined,
  completedByServer: boolean,
): boolean;
export function shouldReconnectCardStream(
  close: {
    acknowledged: boolean;
    completedByServer: boolean;
  },
  validateAccessKey: () => Promise<boolean>,
): Promise<boolean>;
export function createLatestRequestTracker(): {
  next(): number;
  isCurrent(requestGeneration: number): boolean;
  invalidate(): void;
};
export function sortTranscriptEvents(events: TranscriptEvent[]): TranscriptEvent[];
