import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';

export interface MindMapNode {
  id: string;
  title: string;
  content: string;
  files: MindMapNodeFile[];
  changeType: 'unchanged' | 'added' | 'modified' | 'deleted';
}

export interface MindMapNodeFile {
  file: string;
  method: string;
  startLine: number;
  endLine: number;
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
  hasChanges: boolean;
  taskId?: string | null;
  taskStatus: string;
  taskError: string;
  nodes: MindMapNode[];
  edges: MindMapEdge[];
  modifiedNodeIds: string[];
  deletedNodeIds: string[];
}

export interface MindMapUpdate {
  projectId: string;
  sessionId?: string | null;
  updatedAt: string;
}

export interface MindMapSearchResult {
  projectId: string;
  query: string;
  matches: Array<{ nodeId: string; sessionId?: string | null }>;
}

interface MindMapGraphPage extends MindMapGraph {
  nextNodeCursor?: string | null;
  nextEdgeCursor?: string | null;
}

export interface MindMapOperation {
  kind: 'upsert_node' | 'delete_node' | 'upsert_edge' | 'delete_edge';
  id: string;
  title?: string;
  content?: string;
  files?: MindMapNodeFile[];
  sourceId?: string;
  targetId?: string;
  label?: string;
}

const graphPageFields = `
  projectId
  sessionId
  nodes { id title content files { file method startLine endLine } changeType }
  edges { id sourceId targetId label }
  updatedAt
  nextNodeCursor
  nextEdgeCursor
`;

const cardFields = `
  sessionId
  requirement
  updatedAt
  hasChanges
  taskId
  taskStatus
  taskError
  nodes { id title content files { file method startLine endLine } changeType }
  edges { id sourceId targetId label }
  modifiedNodeIds
  deletedNodeIds
`;

export async function getProjectMindMap(projectId: string, sessionId = '') {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    const nodes: MindMapNode[] = [];
    const edges: MindMapEdge[] = [];
    let nodeAfter = '';
    let edgeAfter = '';
    let includeNodes = true;
    let includeEdges = true;
    let revision = '';
    let stable = true;
    do {
      const input = {
        projectId,
        ...(sessionId ? { sessionId } : {}),
        ...(nodeAfter ? { nodeAfter } : {}),
        ...(edgeAfter ? { edgeAfter } : {}),
        includeNodes,
        includeEdges,
        pageSize: 200,
      };
      const data = await graphqlFetch<
        { projectMindMap: MindMapGraphPage },
        { input: typeof input }
      >({
        query: `
          query ProjectMindMap($input: MindMapPageInput!) {
            projectMindMap(input: $input) { ${graphPageFields} }
          }
        `,
        variables: { input },
      });
      const page = data.projectMindMap;
      if (revision && page.updatedAt !== revision) {
        stable = false;
        break;
      }
      revision = page.updatedAt;
      nodes.push(...page.nodes);
      edges.push(...page.edges);
      nodeAfter = page.nextNodeCursor ?? '';
      edgeAfter = page.nextEdgeCursor ?? '';
      includeNodes = Boolean(nodeAfter);
      includeEdges = Boolean(edgeAfter);
    } while (includeNodes || includeEdges);
    if (stable) {
      return { projectId, sessionId: sessionId || null, nodes, edges, updatedAt: revision };
    }
  }
  throw new Error('思维图在分页加载期间持续变化，请稍后重试');
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

export async function searchProjectMindMap(projectId: string, query: string) {
  const input = { projectId, query };
  const data = await graphqlFetch<
    { searchProjectMindMap: MindMapSearchResult },
    { input: typeof input }
  >({
    query: `
      query SearchProjectMindMap($input: SearchMindMapInput!) {
        searchProjectMindMap(input: $input) {
          projectId
          query
          matches { nodeId sessionId }
        }
      }
    `,
    variables: { input },
  });
  return data.searchProjectMindMap;
}

export async function listProjectMindMapUpdatedSessionIds(projectId: string) {
  const data = await graphqlFetch<
    { projectMindMapCards: Pick<MindMapCard, 'sessionId' | 'hasChanges'>[] },
    { projectId: string }
  >({
    query: `
      query ProjectMindMapUpdatedSessionIds($projectId: ID!) {
        projectMindMapCards(projectId: $projectId) { sessionId hasChanges }
      }
    `,
    variables: { projectId },
  });
  return data.projectMindMapCards.filter((card) => card.hasChanges).map((card) => card.sessionId);
}

export async function updateProjectMindMap(input: {
  projectId: string;
  sessionId?: string;
  operations: MindMapOperation[];
}) {
  const data = await graphqlFetch<{ updateProjectMindMap: MindMapUpdate }, { input: typeof input }>(
    {
      query: `
      mutation UpdateProjectMindMap($input: UpdateMindMapInput!) {
        updateProjectMindMap(input: $input) { projectId sessionId updatedAt }
      }
    `,
      variables: { input },
    },
  );
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
