import type { SessionEvent } from '@/services/sessions';

export function appendLiveEvent(events: SessionEvent[], event: SessionEvent): SessionEvent[];
export function prependOlderEvents(
  events: SessionEvent[],
  olderEvents: SessionEvent[],
): SessionEvent[];
export function mergeSnapshotEvents(
  snapshotEvents: SessionEvent[],
  currentEvents: SessionEvent[],
  bufferedEvents: SessionEvent[],
): SessionEvent[];
export function shouldReconnectAfterClose(
  acknowledged: boolean,
  accessKeyValid: boolean | undefined,
): boolean;
export function createLatestRequestTracker(): {
  next(): number;
  isCurrent(requestGeneration: number): boolean;
  invalidate(): void;
};
export function sortSessionEvents(events: SessionEvent[]): SessionEvent[];
