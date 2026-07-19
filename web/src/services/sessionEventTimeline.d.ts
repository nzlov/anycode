import type { TranscriptEvent } from '@/services/sessionTimeline';

export function appendLiveEvent(
  events: TranscriptEvent[],
  event: TranscriptEvent,
): TranscriptEvent[];
export function prependOlderEvents(
  events: TranscriptEvent[],
  olderEvents: TranscriptEvent[],
): TranscriptEvent[];
export function shouldReconnectSubscription(
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
export function createKeyedLatestRequestTracker(): {
  next(key: string): number;
  isCurrent(key: string, requestGeneration: number): boolean;
  invalidate(key: string): void;
  clear(): void;
};
