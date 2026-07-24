import { graphqlFetch } from '@/services/graphqlClient';

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
