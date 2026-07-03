export const GRAPHQL_ACCESS_KEY_STORAGE_KEY = 'anycode.accessKey';

interface GraphQLErrorPayload {
  message: string;
  extensions?: {
    code?: string;
    category?: string;
    details?: Record<string, unknown>;
    retryable?: boolean;
    userAction?: string;
  };
}

interface GraphQLResponse<TData> {
  data?: TData;
  errors?: GraphQLErrorPayload[];
}

interface GraphQLRequest<TVariables extends Record<string, unknown>> {
  query: string;
  variables?: TVariables;
  operationName?: string;
}

interface GraphQLSubscriptionOptions<
  TData,
  TVariables extends Record<string, unknown> = Record<string, unknown>,
> extends GraphQLRequest<TVariables> {
  onData: (data: TData) => void;
  onError?: (error: Error) => void;
  onClose?: () => void;
}

const graphqlEndpoint = import.meta.env.VITE_GRAPHQL_ENDPOINT ?? '/graphql';

export class AnyCodeGraphQLError extends Error {
  code: string;
  category: string;
  details: Record<string, unknown>;
  retryable: boolean;
  userAction: string;

  constructor(errors: GraphQLErrorPayload[]) {
    super(errors.map((error) => error.message).join('; '));
    this.name = 'AnyCodeGraphQLError';
    const first = errors[0]?.extensions;
    this.code = first?.code ?? 'internal_error';
    this.category = first?.category ?? 'infra_error';
    this.details = first?.details ?? {};
    this.retryable = first?.retryable ?? false;
    this.userAction = first?.userAction ?? '';
  }
}

export function getGraphQLAccessKey() {
  if (typeof window === 'undefined') return '';
  return (
    window.localStorage.getItem(GRAPHQL_ACCESS_KEY_STORAGE_KEY) ??
    window.localStorage.getItem('ANYCODE_ACCESS_KEY') ??
    ''
  );
}

export function setGraphQLAccessKey(accessKey: string) {
  if (typeof window === 'undefined') return;
  if (accessKey.trim() === '') {
    window.localStorage.removeItem(GRAPHQL_ACCESS_KEY_STORAGE_KEY);
    return;
  }
  window.localStorage.setItem(GRAPHQL_ACCESS_KEY_STORAGE_KEY, accessKey.trim());
}

function graphqlHeaders(contentType?: string) {
  const headers = new Headers();
  if (contentType) {
    headers.set('content-type', contentType);
  }
  const accessKey = getGraphQLAccessKey();
  if (accessKey) {
    headers.set('authorization', `Bearer ${accessKey}`);
  }
  return headers;
}

async function parseGraphQLResponse<TData>(response: Response) {
  if (!response.ok) {
    throw new Error(`GraphQL request failed: ${response.status}`);
  }

  const payload = (await response.json()) as GraphQLResponse<TData>;
  if (payload.errors?.length) {
    throw new AnyCodeGraphQLError(payload.errors);
  }
  if (!payload.data) {
    throw new Error('GraphQL response missing data');
  }
  return payload.data;
}

export async function graphqlFetch<
  TData,
  TVariables extends Record<string, unknown> = Record<string, unknown>,
>({ query, variables, operationName }: GraphQLRequest<TVariables>) {
  const response = await fetch(graphqlEndpoint, {
    method: 'POST',
    headers: graphqlHeaders('application/json'),
    body: JSON.stringify({ query, variables, operationName }),
  });
  return parseGraphQLResponse<TData>(response);
}

export async function graphqlMultipartFetch<TData>(body: FormData) {
  const response = await fetch(graphqlEndpoint, {
    method: 'POST',
    headers: graphqlHeaders(),
    body,
  });
  return parseGraphQLResponse<TData>(response);
}

export function graphqlSubscribe<
  TData,
  TVariables extends Record<string, unknown> = Record<string, unknown>,
>({
  query,
  variables,
  operationName,
  onData,
  onError,
  onClose,
}: GraphQLSubscriptionOptions<TData, TVariables>) {
  if (typeof window === 'undefined') {
    throw new Error('GraphQL subscriptions require a browser runtime');
  }
  const socket = new WebSocket(graphqlWebSocketURL(), 'graphql-transport-ws');
  const subscriptionID = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
  let completed = false;
  let acknowledged = false;

  socket.addEventListener('open', () => {
    socket.send(
      JSON.stringify({
        type: 'connection_init',
        payload: connectionInitPayload(),
      }),
    );
  });

  socket.addEventListener('message', (event) => {
    const message = parseSocketMessage(event.data);
    if (!message) return;
    if (message.type === 'connection_ack') {
      acknowledged = true;
      socket.send(
        JSON.stringify({
          id: subscriptionID,
          type: 'subscribe',
          payload: { query, variables, operationName },
        }),
      );
      return;
    }
    if (message.type === 'next' && message.id === subscriptionID) {
      const payload = message.payload as GraphQLResponse<TData>;
      if (payload.errors?.length) {
        onError?.(new AnyCodeGraphQLError(payload.errors));
        return;
      }
      if (payload.data) {
        onData(payload.data);
      }
      return;
    }
    if (message.type === 'error' && message.id === subscriptionID) {
      onError?.(new Error(JSON.stringify(message.payload ?? 'GraphQL subscription error')));
      return;
    }
    if (message.type === 'complete' && message.id === subscriptionID) {
      completed = true;
      socket.close();
      return;
    }
    if (message.type === 'ping') {
      socket.send(JSON.stringify({ type: 'pong' }));
    }
  });

  socket.addEventListener('error', () => {
    if (!completed) {
      onError?.(new Error('GraphQL subscription connection failed'));
    }
  });

  socket.addEventListener('close', () => {
    if (!completed && acknowledged) {
      onClose?.();
    }
  });

  return {
    unsubscribe() {
      completed = true;
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ id: subscriptionID, type: 'complete' }));
      }
      socket.close();
    },
  };
}

function graphqlWebSocketURL() {
  const endpoint = graphqlEndpoint;
  if (endpoint.startsWith('ws://') || endpoint.startsWith('wss://')) {
    return endpoint;
  }
  const base = new URL(endpoint, window.location.href);
  base.protocol = base.protocol === 'https:' ? 'wss:' : 'ws:';
  return base.toString();
}

function connectionInitPayload() {
  const accessKey = getGraphQLAccessKey();
  if (!accessKey) return {};
  return { Authorization: `Bearer ${accessKey}` };
}

function parseSocketMessage(data: unknown): { id?: string; type: string; payload?: unknown } | null {
  if (typeof data !== 'string') return null;
  try {
    const parsed = JSON.parse(data) as { id?: string; type?: string; payload?: unknown };
    if (!parsed.type) return null;
    const message: { id?: string; type: string; payload?: unknown } = { type: parsed.type };
    if (parsed.id) {
      message.id = parsed.id;
    }
    if (parsed.payload !== undefined) {
      message.payload = parsed.payload;
    }
    return message;
  } catch {
    return null;
  }
}
