export interface TranscriptPageInput {
  sessionId: string;
  beforeEventId?: string;
  messageRole?: string;
  limit: number;
}

export interface TranscriptPageInfo {
  nextCursor: string;
}

export function latestTranscriptPageInput(
  sessionId: string,
  beforeEventId: string,
  limit: number,
  messageRole = '',
): TranscriptPageInput {
  const input: TranscriptPageInput = {
    sessionId,
    limit,
  };
  if (beforeEventId) {
    input.beforeEventId = beforeEventId;
  }
  if (messageRole) {
    input.messageRole = messageRole;
  }
  return input;
}

export function olderTranscriptCursor(pageInfo: TranscriptPageInfo): string | null {
  return pageInfo.nextCursor || null;
}
