import type { SessionEvent, SessionEventImage } from '@/services/sessions';

export function codexCommandResultBody(
  item: Record<string, unknown>,
  normalizedItem?: Record<string, unknown>,
): string;
export function codexMessageImages(item: Record<string, unknown>): SessionEventImage[];
export function prepareTerminalOutput(value: unknown): string;
export function compactEventPayload(payload: Record<string, unknown>): string;
export function renderMarkdown(markdown: string): string;
export function mergeSessionEvents(events: SessionEvent[]): SessionEvent[];
