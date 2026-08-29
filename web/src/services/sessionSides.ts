import {
  graphqlFetch,
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
}

export async function startSessionSide(sessionId: string, prompt: string) {
  const data = await graphqlFetch<
    { startSessionSide: SessionSideRun },
    { input: { sessionId: string; prompt: string } }
  >({
    query: `
      mutation StartSessionSide($input: StartSessionSideInput!) {
        startSessionSide(input: $input) { codexSessionId processRunId turnId }
      }
    `,
    variables: { input: { sessionId, prompt } },
  });
  return data.startSessionSide;
}

export async function continueSessionSide(
  sessionId: string,
  codexSessionId: string,
  prompt: string,
) {
  const data = await graphqlFetch<
    { continueSessionSide: SessionSideRun },
    { input: { sessionId: string; codexSessionId: string; prompt: string } }
  >({
    query: `
      mutation ContinueSessionSide($input: ContinueSessionSideInput!) {
        continueSessionSide(input: $input) { codexSessionId processRunId turnId }
      }
    `,
    variables: { input: { sessionId, codexSessionId, prompt } },
  });
  return data.continueSessionSide;
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
