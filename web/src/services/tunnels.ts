import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';

export interface Tunnel {
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
  eventType: string;
  runningCount: number;
}

export async function listTunnels() {
  const data = await graphqlFetch<{ tunnels: Tunnel[] }>({
    query: `
      query Tunnels {
        tunnels {
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
