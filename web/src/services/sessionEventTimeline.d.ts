import type { SessionEvent } from '@/services/sessions';

export function eventAfterId(events: SessionEvent[]): string;
export function appendLiveEvent(events: SessionEvent[], event: SessionEvent): SessionEvent[];
export function prependOlderEvents(events: SessionEvent[], olderEvents: SessionEvent[]): SessionEvent[];
export function shouldRefreshSessionForEvent(
  event: SessionEvent,
  liveOnly: boolean,
  replayStateCanRefresh?: boolean,
): boolean;
export function isEventAtOrAfter(event: Pick<SessionEvent, 'createdAt'>, timestamp: number): boolean;
