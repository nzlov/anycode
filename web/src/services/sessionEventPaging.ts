export interface TranscriptPageInput {
  sessionId: string;
  beforeCursor?: string;
  messageRole?: string;
  limit: number;
}

export interface TranscriptPageInfo {
  nextCursor: string;
}

export function latestTranscriptPageInput(
  sessionId: string,
  beforeCursor: string,
  limit: number,
  messageRole = '',
): TranscriptPageInput {
  const input: TranscriptPageInput = {
    sessionId,
    limit,
  };
  if (beforeCursor) {
    input.beforeCursor = beforeCursor;
  }
  if (messageRole) {
    input.messageRole = messageRole;
  }
  return input;
}

export function olderTranscriptCursor(pageInfo: TranscriptPageInfo): string | null {
  return pageInfo.nextCursor || null;
}
