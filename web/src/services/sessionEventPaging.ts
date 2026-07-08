export interface SessionEventPageInput {
  sessionId: string;
  beforeEventId?: string;
  limit: number;
}

export interface SessionEventPageInfo {
  nextCursor: string;
}

export function latestSessionEventPageInput(
  sessionId: string,
  beforeEventId: string,
  limit: number,
): SessionEventPageInput {
  const input: SessionEventPageInput = {
    sessionId,
    limit,
  };
  if (beforeEventId) {
    input.beforeEventId = beforeEventId;
  }
  return input;
}

export function olderSessionEventCursor(pageInfo: SessionEventPageInfo): string | null {
  return pageInfo.nextCursor || null;
}
