export const GRAPHQL_ACCESS_KEY_STORAGE_KEY = 'anycode.accessKey';

interface GraphQLErrorPayload {
  message: string;
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

const graphqlEndpoint = import.meta.env.VITE_GRAPHQL_ENDPOINT ?? '/graphql';

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
    throw new Error(payload.errors.map((error) => error.message).join('; '));
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
