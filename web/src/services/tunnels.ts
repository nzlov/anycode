import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';

export type TunnelMode = 'local' | 'cf';

export interface Tunnel {
  mode: TunnelMode;
  id: string;
  sessionId: string;
  name: string;
  port: number;
  hostname: string;
  url: string;
  accessUrl: string;
  status: string;
  createdAt: string;
}

export interface TunnelCountUpdate {
  tunnelId?: string | null;
  eventType: string;
  runningCount: number | null;
}

export async function listTunnels(ids?: string[]) {
  const data = await graphqlFetch<{ tunnels: Tunnel[] }, { ids: string[] | null }>({
    query: `
      query Tunnels($ids: [ID!]) {
        tunnels(ids: $ids) {
          mode
          id
          sessionId
          name
          port
          hostname
          url
          accessUrl
          status
          createdAt
        }
      }
    `,
    variables: { ids: ids ?? null },
    notify: false,
  });
  return data.tunnels;
}

export async function closeTunnel(id: string) {
  const data = await graphqlFetch<{ closeTunnel: boolean }, { id: string }>({
    query: `
      mutation CloseTunnel($id: ID!) {
        closeTunnel(id: $id)
      }
    `,
    variables: { id },
    notify: false,
  });
  return data.closeTunnel;
}

export function subscribeTunnelUpdates(handlers: {
  onData: (update: TunnelCountUpdate) => void;
  onStart?: () => void;
  onError?: (error: Error) => void;
  onClose?: (close: GraphQLSubscriptionClose) => void;
}) {
  return graphqlSubscribe<{ tunnelUpdates: TunnelCountUpdate }>({
    query: `
      subscription TunnelUpdates {
        tunnelUpdates {
          tunnelId
          eventType
          runningCount
        }
      }
    `,
    onData: (data) => handlers.onData(data.tunnelUpdates),
    ...(handlers.onStart ? { onStart: handlers.onStart } : {}),
    ...(handlers.onError ? { onError: handlers.onError } : {}),
    ...(handlers.onClose ? { onClose: handlers.onClose } : {}),
  });
}

export async function getTunnelMode() {
  const data = await graphqlFetch<{ tunnelMode: TunnelMode }>({
    query: `query TunnelMode { tunnelMode }`,
    notify: false,
  });
  return data.tunnelMode;
}

export async function setTunnelMode(mode: TunnelMode) {
  const data = await graphqlFetch<{ setTunnelMode: TunnelMode }, { mode: TunnelMode }>({
    query: `mutation SetTunnelMode($mode: String!) { setTunnelMode(mode: $mode) }`,
    variables: { mode },
    notify: false,
  });
  return data.setTunnelMode;
}

export async function switchTunnelMode(id: string, mode: TunnelMode) {
  await graphqlFetch<{ switchTunnelMode: boolean }, { id: string; mode: TunnelMode }>({
    query: `mutation SwitchTunnelMode($id: ID!, $mode: String!) { switchTunnelMode(id: $id, mode: $mode) }`,
    variables: { id, mode },
    notify: false,
  });
}
