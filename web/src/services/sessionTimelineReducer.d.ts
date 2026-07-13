import type { TranscriptEvent, TranscriptItem } from '@/services/sessionTimeline';

export function reduceTranscriptEvents(events: TranscriptEvent[]): TranscriptItem[];
