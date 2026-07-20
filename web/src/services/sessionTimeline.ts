import { graphqlFetch } from '@/services/graphqlClient';
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
  kind: 'exec' | 'shell';
  commands: TranscriptCommandInvocation[];
  durationMs: number | null;
}

export interface TranscriptCommandInvocation {
  command: string;
  workdir: string;
  hasOutput: boolean;
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
  group?: TranscriptEventGroup | null;
}

export interface TranscriptEventGroup {
  kind: string;
  label: string;
  count: number;
  members: TranscriptEvent[];
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
  currentInputTokens: number;
  currentCachedInputTokens: number;
  currentOutputTokens: number;
  currentReasoningOutputTokens: number;
  currentTotalTokens: number;
  compactionCount: number;
}

export interface TranscriptUsageAttribution {
  processRunId?: string | null;
  nodeRunId?: string | null;
  usage: TranscriptTokenUsage;
}

export interface GraphQLTranscriptEvent extends Omit<TranscriptEvent, 'phase' | 'content'> {
  phase: string;
  content: TranscriptContent;
}

const transcriptContentFields = `
  __typename
  ... on TranscriptMessageContent {
    role
    text
    format
    images { src detail }
  }
  ... on TranscriptReasoningContent { text }
  ... on TranscriptCommandContent {
    kind
    commands { command workdir hasOutput output exitCode durationMs }
    durationMs
  }
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
`;

const transcriptEventBaseFields = `
  id
  orderKey
  correlationId
  phase
  occurredAt
  content { ${transcriptContentFields} }
`;

export const transcriptEventFields = `
  ${transcriptEventBaseFields}
  group {
    kind
    label
    count
    members { ${transcriptEventBaseFields} }
  }
`;

export const transcriptUsageFields = `
  inputTokens
  cachedInputTokens
  outputTokens
  reasoningOutputTokens
  totalTokens
  contextWindow
  currentInputTokens
  currentCachedInputTokens
  currentOutputTokens
  currentReasoningOutputTokens
  currentTotalTokens
  compactionCount
`;

export async function getSessionTranscriptPage(
  sessionId: string,
  beforeCursor: string,
  limit: number,
  messageRole = '',
) {
  const data = await graphqlFetch<
    {
      sessionTranscript: {
        events: GraphQLTranscriptEvent[];
        usage?: TranscriptTokenUsage | null;
        processUsage: TranscriptUsageAttribution[];
        nodeUsage: TranscriptUsageAttribution[];
        pageInfo: PageInfo;
      };
    },
    { input: { sessionId: string; beforeCursor?: string; messageRole?: string; limit: number } }
  >({
    query: `
      query SessionTranscript($input: ListTranscriptEventsInput!) {
        sessionTranscript(input: $input) {
          events { ${transcriptEventFields} }
          usage { ${transcriptUsageFields} }
          processUsage { processRunId nodeRunId usage { ${transcriptUsageFields} } }
          nodeUsage { processRunId nodeRunId usage { ${transcriptUsageFields} } }
          pageInfo { page pageSize total nextCursor }
        }
      }
    `,
    variables: { input: latestTranscriptPageInput(sessionId, beforeCursor, limit, messageRole) },
  });
  return {
    items: data.sessionTranscript.events.map(normalizeTranscriptEvent),
    usage: data.sessionTranscript.usage ?? null,
    processUsage: data.sessionTranscript.processUsage,
    nodeUsage: data.sessionTranscript.nodeUsage,
    pageInfo: data.sessionTranscript.pageInfo,
  };
}

export function normalizeTranscriptEvent(event: GraphQLTranscriptEvent): TranscriptEvent {
  return {
    ...event,
    correlationId: event.correlationId ?? '',
    phase: event.phase.toLowerCase() as TranscriptPhase,
    content: normalizeContent(event.content),
    group: event.group
      ? {
          ...event.group,
          members: event.group.members.map((member) =>
            normalizeTranscriptEvent(member as GraphQLTranscriptEvent),
          ),
        }
      : null,
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
