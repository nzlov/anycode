export interface TranscriptPageInput {
  sessionId: string;
  beforeEventId?: string;
  limit: number;
}

export interface TranscriptPageInfo {
  nextCursor: string;
}

export function latestTranscriptPageInput(
  sessionId: string,
  beforeEventId: string,
  limit: number,
): TranscriptPageInput {
  const input: TranscriptPageInput = {
    sessionId,
    limit,
  };
  if (beforeEventId) {
    input.beforeEventId = beforeEventId;
  }
  return input;
}

export function olderTranscriptCursor(pageInfo: TranscriptPageInfo): string | null {
  return pageInfo.nextCursor || null;
}
