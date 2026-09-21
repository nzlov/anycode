import {
  graphqlFetch,
  graphqlMultipartFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import {
  normalizeTranscriptEvent,
  transcriptEventFields,
  type GraphQLTranscriptEvent,
  type TranscriptEvent,
} from '@/services/sessionTimeline';

export interface SessionSideRun {
  codexSessionId: string;
  processRunId: string;
  turnId: string;
  prompt: string;
  followUps: string[];
  status: 'running' | 'completed' | 'failed';
  error: string;
  events: TranscriptEvent[];
}

export interface SessionSideConfig {
  codexModel: string;
  reasoningEffort: string;
  fastMode: boolean;
}

export interface SessionSideMessage {
  prompt: string;
  files: File[];
  config: SessionSideConfig;
}

const sessionSideFields = `
  codexSessionId
  processRunId
  turnId
  prompt
  followUps
  status
  error
  events { ${transcriptEventFields} }
`;

type GraphQLSessionSideRun = Omit<SessionSideRun, 'events'> & { events: GraphQLTranscriptEvent[] };

function normalizeSessionSide(run: GraphQLSessionSideRun): SessionSideRun {
  return { ...run, events: run.events.map(normalizeTranscriptEvent) };
}

export async function listSessionSides(sessionId: string) {
  const data = await graphqlFetch<{ sessionSides: GraphQLSessionSideRun[] }, { sessionId: string }>({
    query: `
      query SessionSides($sessionId: ID!) {
        sessionSides(sessionId: $sessionId) { ${sessionSideFields} }
      }
    `,
    variables: { sessionId },
  });
  return data.sessionSides.map(normalizeSessionSide);
}

async function sendSideMessage<TData>(
  query: string,
  input: { sessionId: string; codexSessionId?: string; prompt: string; config: SessionSideConfig },
  files: File[],
) {
  const { codexModel, reasoningEffort, fastMode } = input.config;
  input = { ...input, config: { codexModel, reasoningEffort, fastMode } };
  if (!files.length) return graphqlFetch<TData>({ query, variables: { input } });
  const body = new FormData();
  body.append(
    'operations',
    JSON.stringify({ query, variables: { input: { ...input, files: files.map(() => null) } } }),
  );
  body.append(
    'map',
    JSON.stringify(
      Object.fromEntries(
        files.map((_, index) => [String(index), [`variables.input.files.${index}`]]),
      ),
    ),
  );
  files.forEach((file, index) => body.append(String(index), file, file.name));
  return graphqlMultipartFetch<TData>(body);
}

export async function startSessionSide(sessionId: string, message: SessionSideMessage) {
  const data = await sendSideMessage<{ startSessionSide: GraphQLSessionSideRun }>(
    `
      mutation StartSessionSide($input: StartSessionSideInput!) {
        startSessionSide(input: $input) { ${sessionSideFields} }
      }
    `,
    { sessionId, prompt: message.prompt, config: message.config },
    message.files,
  );
  return normalizeSessionSide(data.startSessionSide);
}

export async function continueSessionSide(
  sessionId: string,
  codexSessionId: string,
  message: SessionSideMessage,
) {
  const data = await sendSideMessage<{ continueSessionSide: GraphQLSessionSideRun }>(
    `
      mutation ContinueSessionSide($input: ContinueSessionSideInput!) {
        continueSessionSide(input: $input) { ${sessionSideFields} }
      }
    `,
    { sessionId, codexSessionId, prompt: message.prompt, config: message.config },
    message.files,
  );
  return normalizeSessionSide(data.continueSessionSide);
}

export async function stopSessionSide(processRunId: string) {
  const data = await graphqlFetch<{ stopSessionSide: boolean }, { processRunId: string }>({
    query: `
      mutation StopSessionSide($processRunId: ID!) {
        stopSessionSide(processRunId: $processRunId)
      }
    `,
    variables: { processRunId },
  });
  return data.stopSessionSide;
}

export function subscribeSessionSideEvents(
  processRunId: string,
  handlers: {
    onData: (event: TranscriptEvent) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
  },
) {
  const options = {
    query: `
			subscription SessionSideEvents($processRunId: ID!) {
				sessionSideEvents(processRunId: $processRunId) { ${transcriptEventFields} }
			}
		`,
    variables: { processRunId },
    onData: (data: { sessionSideEvents: GraphQLTranscriptEvent }) =>
      handlers.onData(normalizeTranscriptEvent(data.sessionSideEvents)),
  };
  if (handlers.onError) Object.assign(options, { onError: handlers.onError });
  if (handlers.onClose) Object.assign(options, { onClose: handlers.onClose });
  return graphqlSubscribe<{ sessionSideEvents: GraphQLTranscriptEvent }, { processRunId: string }>(
    options,
  );
}
