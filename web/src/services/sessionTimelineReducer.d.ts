import type { SessionTimelineEvent, SessionTimelineItem } from '@/services/sessionTimeline';

export function reduceSessionTimelineEvents(events: SessionTimelineEvent[]): SessionTimelineItem[];
