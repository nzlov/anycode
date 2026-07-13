import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { latestTranscriptPageInput } from '@/services/sessionEventPaging';
import type { PageInfo } from '@/services/sessions';

export type TranscriptPhase =
  'standalone' | 'started' | 'progress' | 'completed' | 'failed' | 'cancelled';

export type TranscriptTextFormat = 'plain' | 'markdown' | 'json' | 'ansi';

export interface TranscriptImage {
  src: string;
  detail: string;
}

export interface TranscriptStructuredText {
  format: TranscriptTextFormat;
  text: string;
}

export interface TranscriptMessageContent {
  __typename: 'TranscriptMessageContent';
  role: string;
  text: string;
  format: TranscriptTextFormat;
  images: TranscriptImage[];
}

export interface TranscriptReasoningContent {
  __typename: 'TranscriptReasoningContent';
  text: string;
}

export interface TranscriptCommandContent {
  __typename: 'TranscriptCommandContent';
  command: string;
  output: string;
  exitCode: number | null;
  durationMs: number | null;
}

export interface TranscriptToolContent {
  __typename: 'TranscriptToolContent';
  qualifiedName: string;
  category: string;
  input: TranscriptStructuredText;
  output: TranscriptStructuredText;
  images: TranscriptImage[];
}

export interface TranscriptFileChange {
  kind: string;
  path: string;
  movePath: string;
  unifiedDiff: string;
}

export interface TranscriptFileChangeContent {
  __typename: 'TranscriptFileChangeContent';
  changes: TranscriptFileChange[];
}

export interface TranscriptStatusContent {
  __typename: 'TranscriptStatusContent';
  code: string;
  level: string;
  message: string;
  details: Record<string, unknown>;
}

export interface TranscriptUnknownContent {
  __typename: 'TranscriptUnknownContent';
  rawType: string;
  payload: Record<string, unknown>;
}

export type TranscriptContent =
  | TranscriptMessageContent
  | TranscriptReasoningContent
  | TranscriptCommandContent
  | TranscriptToolContent
  | TranscriptFileChangeContent
  | TranscriptStatusContent
  | TranscriptUnknownContent;

export interface TranscriptEvent {
  id: string;
  orderKey: string;
  correlationId: string;
  phase: TranscriptPhase;
  occurredAt: string;
  content: TranscriptContent;
}

export interface TranscriptItem extends TranscriptEvent {
  sourceEventIds: string[];
}

export interface TranscriptTokenUsage {
  inputTokens: number;
  cachedInputTokens: number;
  outputTokens: number;
  reasoningOutputTokens: number;
  totalTokens: number;
  contextWindow: number;
}

interface GraphQLTranscriptEvent extends Omit<TranscriptEvent, 'phase' | 'content'> {
  phase: string;
  content: TranscriptContent;
}

interface GraphQLTranscriptStreamItem {
  ready: boolean;
  event?: GraphQLTranscriptEvent | null;
  usage?: TranscriptTokenUsage | null;
}

const transcriptEventFields = `
  id
  orderKey
  correlationId
  phase
  occurredAt
  content {
    __typename
    ... on TranscriptMessageContent {
      role
      text
      format
      images { src detail }
    }
    ... on TranscriptReasoningContent { text }
    ... on TranscriptCommandContent { command output exitCode durationMs }
    ... on TranscriptToolContent {
      qualifiedName
      category
      input { format text }
      output { format text }
      images { src detail }
    }
    ... on TranscriptFileChangeContent {
      changes { kind path movePath unifiedDiff }
    }
    ... on TranscriptStatusContent { code level message details }
    ... on TranscriptUnknownContent { rawType payload }
  }
`;

const transcriptUsageFields = `
  inputTokens
  cachedInputTokens
  outputTokens
  reasoningOutputTokens
  totalTokens
  contextWindow
`;

export async function getSessionTranscriptPage(
  sessionId: string,
  beforeEventId: string,
  limit: number,
) {
  const data = await graphqlFetch<
    {
      sessionTranscript: {
        events: GraphQLTranscriptEvent[];
        usage?: TranscriptTokenUsage | null;
        pageInfo: PageInfo;
      };
    },
    { input: { sessionId: string; beforeEventId?: string; limit: number } }
  >({
    query: `
      query SessionTranscript($input: ListTranscriptEventsInput!) {
        sessionTranscript(input: $input) {
          events { ${transcriptEventFields} }
          usage { ${transcriptUsageFields} }
          pageInfo { page pageSize total nextCursor }
        }
      }
    `,
    variables: { input: latestTranscriptPageInput(sessionId, beforeEventId, limit) },
  });
  return {
    items: data.sessionTranscript.events.map(normalizeTranscriptEvent),
    usage: data.sessionTranscript.usage ?? null,
    pageInfo: data.sessionTranscript.pageInfo,
  };
}

export function subscribeSessionTranscript(
  sessionId: string,
  handlers: {
    onData: (event: TranscriptEvent) => void;
    onUsage?: (usage: TranscriptTokenUsage) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
    onSubscribed?: () => void;
  },
) {
  const options = {
    query: `
      subscription SessionTranscript($sessionId: ID!) {
        sessionTranscript(sessionId: $sessionId) {
          ready
          event { ${transcriptEventFields} }
          usage { ${transcriptUsageFields} }
        }
      }
    `,
    variables: { sessionId },
    onData: (data: { sessionTranscript: GraphQLTranscriptStreamItem }) => {
      if (data.sessionTranscript.ready) {
        handlers.onSubscribed?.();
        return;
      }
      if (data.sessionTranscript.event) {
        handlers.onData(normalizeTranscriptEvent(data.sessionTranscript.event));
      }
      if (data.sessionTranscript.usage) {
        handlers.onUsage?.(data.sessionTranscript.usage);
      }
    },
  };
  if (handlers.onError) Object.assign(options, { onError: handlers.onError });
  if (handlers.onClose) Object.assign(options, { onClose: handlers.onClose });
  return graphqlSubscribe<{ sessionTranscript: GraphQLTranscriptStreamItem }, { sessionId: string }>(
    options,
  );
}

function normalizeTranscriptEvent(event: GraphQLTranscriptEvent): TranscriptEvent {
  return {
    ...event,
    correlationId: event.correlationId ?? '',
    phase: event.phase.toLowerCase() as TranscriptPhase,
    content: normalizeContent(event.content),
  };
}

function normalizeContent(content: TranscriptContent): TranscriptContent {
  if (content.__typename === 'TranscriptMessageContent') {
    return { ...content, format: normalizeTranscriptTextFormat(content.format) };
  }
  if (content.__typename === 'TranscriptToolContent') {
    return {
      ...content,
      input: { ...content.input, format: normalizeTranscriptTextFormat(content.input.format) },
      output: { ...content.output, format: normalizeTranscriptTextFormat(content.output.format) },
    };
  }
  return content;
}

function normalizeTranscriptTextFormat(value: string): TranscriptTextFormat {
  return value.toLowerCase() as TranscriptTextFormat;
}
