import type { SessionTimelineEvent } from '@/services/sessionTimeline';

export function appendLiveEvent(
  events: SessionTimelineEvent[],
  event: SessionTimelineEvent,
): SessionTimelineEvent[];
export function prependOlderEvents(
  events: SessionTimelineEvent[],
  olderEvents: SessionTimelineEvent[],
): SessionTimelineEvent[];
export function mergeSnapshotEvents(
  snapshotEvents: SessionTimelineEvent[],
  currentEvents: SessionTimelineEvent[],
  bufferedEvents: SessionTimelineEvent[],
): SessionTimelineEvent[];
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
export function sortSessionEvents(events: SessionTimelineEvent[]): SessionTimelineEvent[];
