import { Notify } from 'quasar';

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
  notify?: boolean;
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

export function clearGraphQLAccessKey() {
  if (typeof window === 'undefined') return;
  window.localStorage.removeItem(GRAPHQL_ACCESS_KEY_STORAGE_KEY);
  window.localStorage.removeItem('ANYCODE_ACCESS_KEY');
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
>({ query, variables, operationName, notify = true }: GraphQLRequest<TVariables>) {
  try {
    const response = await fetch(graphqlEndpoint, {
      method: 'POST',
      headers: graphqlHeaders('application/json'),
      body: JSON.stringify({ query, variables, operationName }),
    });
    return await parseGraphQLResponse<TData>(response);
  } catch (err) {
    if (notify) notifyRequestError(err);
    throw err;
  }
}

export async function graphqlMultipartFetch<TData>(body: FormData) {
  try {
    const response = await fetch(graphqlEndpoint, {
      method: 'POST',
      headers: graphqlHeaders(),
      body,
    });
    return await parseGraphQLResponse<TData>(response);
  } catch (err) {
    notifyRequestError(err);
    throw err;
  }
}

function notifyRequestError(err: unknown) {
  if (typeof window === 'undefined') return;
  if (isNotifiedError(err)) return;
  const message = err instanceof AnyCodeGraphQLError && err.userAction ? err.userAction : errorMessage(err);
  Notify.create({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
  markNotified(err);
}

function errorMessage(err: unknown) {
  if (err instanceof Error) return err.message || '请求失败';
  if (typeof err === 'string') return err || '请求失败';
  return '请求失败';
}

function isNotifiedError(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
}

function markNotified(err: unknown) {
  if (!err || typeof err !== 'object') return;
  Object.defineProperty(err, '__anycodeNotified', {
    configurable: true,
    value: true,
  });
}

export async function verifyGraphQLAccessKey(accessKey: string) {
  const response = await fetch('/api/healthz', {
    headers: { authorization: `Bearer ${accessKey.trim()}` },
  });
  return response.ok;
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
        handleSubscriptionError(new AnyCodeGraphQLError(payload.errors), onError);
        return;
      }
      if (payload.data) {
        onData(payload.data);
      }
      return;
    }
    if (message.type === 'error' && message.id === subscriptionID) {
      handleSubscriptionError(new Error(subscriptionErrorMessage(message.payload)), onError);
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
      handleSubscriptionError(new Error('GraphQL subscription connection failed'), onError);
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

function handleSubscriptionError(error: Error, onError?: (error: Error) => void) {
  notifyRequestError(error);
  onError?.(error);
}

function subscriptionErrorMessage(payload: unknown) {
  if (typeof payload === 'string') return payload;
  if (payload && typeof payload === 'object' && 'message' in payload) {
    const message = (payload as { message?: unknown }).message;
    if (typeof message === 'string' && message) return message;
  }
  return 'GraphQL subscription error';
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
