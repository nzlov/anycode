import type { SessionEventMessageEntry } from '@/components/SessionEventMessage.vue';

export function codexCommandResultBody(item: Record<string, unknown>): string;
export function renderMarkdown(markdown: string): string;
export function mergeShellEvents(events: SessionEventMessageEntry[]): SessionEventMessageEntry[];
