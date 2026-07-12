import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { latestSessionEventPageInput } from '@/services/sessionEventPaging';
import type { PageInfo } from '@/services/sessions';

export type SessionTimelinePhase =
  'standalone' | 'started' | 'progress' | 'completed' | 'failed' | 'cancelled';

export type SessionTimelineTextFormat = 'plain' | 'markdown' | 'json' | 'ansi';

export interface SessionTimelineImage {
  src: string;
  detail: string;
}

export interface SessionStructuredText {
  format: SessionTimelineTextFormat;
  text: string;
}

export interface SessionTextMessageContent {
  __typename: 'SessionTextMessageContent';
  role: string;
  text: string;
  format: SessionTimelineTextFormat;
  images: SessionTimelineImage[];
}

export interface SessionReasoningContent {
  __typename: 'SessionReasoningContent';
  text: string;
}

export interface SessionCommandContent {
  __typename: 'SessionCommandContent';
  command: string;
  output: string;
  exitCode: number | null;
  durationMs: number | null;
}

export interface SessionToolContent {
  __typename: 'SessionToolContent';
  qualifiedName: string;
  category: string;
  input: SessionStructuredText;
  output: SessionStructuredText;
  images: SessionTimelineImage[];
}

export interface SessionTimelineFileChange {
  kind: string;
  path: string;
  movePath: string;
  unifiedDiff: string;
}

export interface SessionFileChangeContent {
  __typename: 'SessionFileChangeContent';
  changes: SessionTimelineFileChange[];
}

export interface SessionStatusContent {
  __typename: 'SessionStatusContent';
  code: string;
  level: string;
  message: string;
  details: Record<string, unknown>;
}

export interface SessionUnknownContent {
  __typename: 'SessionUnknownContent';
  rawType: string;
  payload: Record<string, unknown>;
}

export type SessionTimelineContent =
  | SessionTextMessageContent
  | SessionReasoningContent
  | SessionCommandContent
  | SessionToolContent
  | SessionFileChangeContent
  | SessionStatusContent
  | SessionUnknownContent;

export interface SessionTimelineEvent {
  id: string;
  orderKey: string;
  correlationId: string;
  phase: SessionTimelinePhase;
  occurredAt: string;
  content: SessionTimelineContent;
}

export interface SessionTimelineItem extends SessionTimelineEvent {
  sourceEventIds: string[];
}

export interface SessionTokenUsage {
  inputTokens: number;
  cachedInputTokens: number;
  outputTokens: number;
  reasoningOutputTokens: number;
  totalTokens: number;
  contextWindow: number;
}

interface GraphQLTimelineEvent extends Omit<SessionTimelineEvent, 'phase' | 'content'> {
  phase: string;
  content: SessionTimelineContent;
}

interface GraphQLTimelineStreamItem {
  ready: boolean;
  event?: GraphQLTimelineEvent | null;
  usage?: SessionTokenUsage | null;
}

const timelineEventFields = `
  id
  orderKey
  correlationId
  phase
  occurredAt
  content {
    __typename
    ... on SessionTextMessageContent {
      role
      text
      format
      images { src detail }
    }
    ... on SessionReasoningContent { text }
    ... on SessionCommandContent { command output exitCode durationMs }
    ... on SessionToolContent {
      qualifiedName
      category
      input { format text }
      output { format text }
      images { src detail }
    }
    ... on SessionFileChangeContent {
      changes { kind path movePath unifiedDiff }
    }
    ... on SessionStatusContent { code level message details }
    ... on SessionUnknownContent { rawType payload }
  }
`;

const tokenUsageFields = `
  inputTokens
  cachedInputTokens
  outputTokens
  reasoningOutputTokens
  totalTokens
  contextWindow
`;

export async function getSessionTimelinePage(
  sessionId: string,
  beforeEventId: string,
  limit: number,
) {
  const data = await graphqlFetch<
    {
      sessionEvents: {
        events: GraphQLTimelineEvent[];
        usage?: SessionTokenUsage | null;
        pageInfo: PageInfo;
      };
    },
    { input: { sessionId: string; beforeEventId?: string; limit: number } }
  >({
    query: `
      query SessionEvents($input: ListSessionEventsInput!) {
        sessionEvents(input: $input) {
          events { ${timelineEventFields} }
          usage { ${tokenUsageFields} }
          pageInfo { page pageSize total nextCursor }
        }
      }
    `,
    variables: { input: latestSessionEventPageInput(sessionId, beforeEventId, limit) },
  });
  return {
    items: data.sessionEvents.events.map(normalizeTimelineEvent),
    usage: data.sessionEvents.usage ?? null,
    pageInfo: data.sessionEvents.pageInfo,
  };
}

export function subscribeSessionTimeline(
  sessionId: string,
  handlers: {
    onData: (event: SessionTimelineEvent) => void;
    onUsage?: (usage: SessionTokenUsage) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
    onSubscribed?: () => void;
  },
) {
  const options = {
    query: `
      subscription SessionEvents($sessionId: ID!) {
        sessionEvents(sessionId: $sessionId) {
          ready
          event { ${timelineEventFields} }
          usage { ${tokenUsageFields} }
        }
      }
    `,
    variables: { sessionId },
    onData: (data: { sessionEvents: GraphQLTimelineStreamItem }) => {
      if (data.sessionEvents.ready) {
        handlers.onSubscribed?.();
        return;
      }
      if (data.sessionEvents.event) {
        handlers.onData(normalizeTimelineEvent(data.sessionEvents.event));
      }
      if (data.sessionEvents.usage) {
        handlers.onUsage?.(data.sessionEvents.usage);
      }
    },
  };
  if (handlers.onError) Object.assign(options, { onError: handlers.onError });
  if (handlers.onClose) Object.assign(options, { onClose: handlers.onClose });
  return graphqlSubscribe<{ sessionEvents: GraphQLTimelineStreamItem }, { sessionId: string }>(
    options,
  );
}

function normalizeTimelineEvent(event: GraphQLTimelineEvent): SessionTimelineEvent {
  return {
    ...event,
    correlationId: event.correlationId ?? '',
    phase: event.phase.toLowerCase() as SessionTimelinePhase,
    content: normalizeContent(event.content),
  };
}

function normalizeContent(content: SessionTimelineContent): SessionTimelineContent {
  if (content.__typename === 'SessionTextMessageContent') {
    return { ...content, format: normalizeTextFormat(content.format) };
  }
  if (content.__typename === 'SessionToolContent') {
    return {
      ...content,
      input: { ...content.input, format: normalizeTextFormat(content.input.format) },
      output: { ...content.output, format: normalizeTextFormat(content.output.format) },
    };
  }
  return content;
}

function normalizeTextFormat(value: string): SessionTimelineTextFormat {
  return value.toLowerCase() as SessionTimelineTextFormat;
}
