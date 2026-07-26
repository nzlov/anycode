import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';

export interface MindMapNode {
  id: string;
  title: string;
  content: string;
}

export interface MindMapEdge {
  id: string;
  sourceId: string;
  targetId: string;
  label: string;
}

export interface MindMapGraph {
  projectId: string;
  sessionId?: string | null;
  nodes: MindMapNode[];
  edges: MindMapEdge[];
  updatedAt: string;
}

export interface MindMapCard {
  sessionId: string;
  requirement: string;
  updatedAt: string;
  taskId?: string | null;
  taskStatus: string;
  taskError: string;
}

export interface MindMapUpdate {
  projectId: string;
  sessionId?: string | null;
  updatedAt: string;
}

export interface MindMapOperation {
  kind: 'upsert_node' | 'delete_node' | 'upsert_edge' | 'delete_edge';
  id: string;
  title?: string;
  content?: string;
  sourceId?: string;
  targetId?: string;
  label?: string;
}

const graphFields = `
  projectId
  sessionId
  nodes { id title content }
  edges { id sourceId targetId label }
  updatedAt
`;

const cardFields = `
  sessionId
  requirement
  updatedAt
  taskId
  taskStatus
  taskError
`;

export async function getProjectMindMap(projectId: string, sessionId = '') {
  const data = await graphqlFetch<
    { projectMindMap: MindMapGraph },
    { projectId: string; sessionId?: string }
  >({
    query: `
      query ProjectMindMap($projectId: ID!, $sessionId: ID) {
        projectMindMap(projectId: $projectId, sessionId: $sessionId) { ${graphFields} }
      }
    `,
    variables: { projectId, ...(sessionId ? { sessionId } : {}) },
  });
  return data.projectMindMap;
}

export async function listProjectMindMapCards(projectId: string) {
  const data = await graphqlFetch<{ projectMindMapCards: MindMapCard[] }, { projectId: string }>({
    query: `
      query ProjectMindMapCards($projectId: ID!) {
        projectMindMapCards(projectId: $projectId) { ${cardFields} }
      }
    `,
    variables: { projectId },
  });
  return data.projectMindMapCards;
}

export async function updateProjectMindMap(input: {
  projectId: string;
  sessionId?: string;
  operations: MindMapOperation[];
}) {
  const data = await graphqlFetch<
    { updateProjectMindMap: MindMapGraph },
    { input: typeof input }
  >({
    query: `
      mutation UpdateProjectMindMap($input: UpdateMindMapInput!) {
        updateProjectMindMap(input: $input) { ${graphFields} }
      }
    `,
    variables: { input },
  });
  return data.updateProjectMindMap;
}

export async function retryMindMapTask(id: string) {
  const data = await graphqlFetch<{ retryMindMapTask: MindMapCard }, { id: string }>({
    query: `
      mutation RetryMindMapTask($id: ID!) {
        retryMindMapTask(id: $id) { ${cardFields} }
      }
    `,
    variables: { id },
  });
  return data.retryMindMapTask;
}

export function subscribeMindMapUpdates(
  projectId: string,
  sessionId: string,
  handlers: {
    onData: (update: MindMapUpdate) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
  },
) {
  return graphqlSubscribe<
    { mindMapUpdates: MindMapUpdate },
    { projectId: string; sessionId?: string }
  >({
    query: `
      subscription MindMapUpdates($projectId: ID!, $sessionId: ID) {
        mindMapUpdates(projectId: $projectId, sessionId: $sessionId) {
          projectId
          sessionId
          updatedAt
        }
      }
    `,
    variables: { projectId, ...(sessionId ? { sessionId } : {}) },
    onData: (data) => handlers.onData(data.mindMapUpdates),
    ...(handlers.onError ? { onError: handlers.onError } : {}),
    ...(handlers.onClose ? { onClose: handlers.onClose } : {}),
  });
}
