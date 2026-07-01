import { sessions as mockSessions } from '@/mocks/workbench';
import { graphqlFetch } from '@/services/graphqlClient';

export type SessionMode = 'workflow' | 'chat';
export type SessionStatus =
  | 'created'
  | 'starting'
  | 'running'
  | 'waiting_user'
  | 'stopping'
  | 'stopped'
  | 'resume_failed'
  | 'failed'
  | 'blocked'
  | 'completed'
  | 'closed';

export interface SessionCard {
  id: string;
  projectId: string;
  title: string;
  summary: string;
  mode: SessionMode;
  status: SessionStatus;
  branch: string;
  node: string;
  updatedAt: string;
  pendingQuestion: boolean;
  filesChanged: number;
}

export interface SessionDetail extends SessionCard {
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  availableActions: string[];
  canResume: boolean;
}

export interface SessionEvent {
  id: string;
  kind: 'thought' | 'tool' | 'assistant' | 'status' | 'question';
  title: string;
  body: string;
  time: string;
}

export interface PageInfo {
  page: number;
  pageSize: number;
  total: number;
  nextCursor: string;
}

export interface ListSessionsInput {
  projectId?: string;
  scope?: string;
  range?: string;
  page?: number;
  pageSize?: number;
  filter?: string;
  sort?: string;
}

export interface SessionPage {
  items: SessionCard[];
  pageInfo: PageInfo;
}

export interface SessionDetailData {
  session: SessionDetail;
  events: SessionEvent[];
}

export interface CreateSessionInput {
  projectId: string;
  requirement: string;
  mode: SessionMode;
  baseBranch?: string;
  config?: {
    codexModel?: string;
    reasoningEffort?: string;
    permissionMode?: string;
  };
  stagedAttachmentIds?: string[];
}

interface GraphQLPageInfo {
  page: number;
  pageSize: number;
  total: number;
  nextCursor: string;
}

interface GraphQLSessionCard {
  id: string;
  projectId: string;
  projectName: string;
  requirement: string;
  requirementSummary: string;
  mode: string;
  status: string;
  baseBranch: string;
  currentNodeTitle: string;
  pendingQuestion: boolean;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLSessionDetail {
  id: string;
  projectId: string;
  requirement: string;
  mode: string;
  status: string;
  baseBranch: string;
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  availableActions: string[];
  canResume: boolean;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLSession {
  id: string;
  projectId: string;
  requirement: string;
  mode: string;
  status: string;
  baseBranch: string;
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLSessionEvent {
  id: string;
  type: string;
  payload: Record<string, unknown>;
  createdAt: string;
}

const sessionCardFields = `
  id
  projectId
  projectName
  requirement
  requirementSummary
  mode
  status
  baseBranch
  currentNodeTitle
  pendingQuestion
  lastRunAt
  createdAt
  updatedAt
`;

const sessionDetailFields = `
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  availableActions
  canResume
  lastRunAt
  createdAt
  updatedAt
`;

const sessionFields = `
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  lastRunAt
  createdAt
  updatedAt
`;

export async function listSessions(input: ListSessionsInput = {}): Promise<SessionPage> {
  try {
    const data = await graphqlFetch<
      { sessions: { items: GraphQLSessionCard[]; pageInfo: GraphQLPageInfo } },
      { input: ListSessionsInput }
    >({
      query: `
        query Sessions($input: ListSessionsInput) {
          sessions(input: $input) {
            items {
              ${sessionCardFields}
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,
      variables: { input },
    });
    return {
      items: data.sessions.items.map(normalizeSessionCard),
      pageInfo: data.sessions.pageInfo,
    };
  } catch {
    return mockSessionPage(input);
  }
}

export async function getSessionDetail(sessionId: string): Promise<SessionDetailData> {
  const [sessionData, eventsData] = await Promise.all([
    graphqlFetch<{ session: GraphQLSessionDetail }, { id: string }>({
      query: `
        query Session($id: ID!) {
          session(id: $id) {
            ${sessionDetailFields}
          }
        }
      `,
      variables: { id: sessionId },
    }),
    graphqlFetch<
      { sessionEvents: { items: GraphQLSessionEvent[]; pageInfo: GraphQLPageInfo } },
      { input: { sessionId: string; page: number; pageSize: number } }
    >({
      query: `
        query SessionEvents($input: ListSessionEventsInput!) {
          sessionEvents(input: $input) {
            items {
              id
              type
              payload
              createdAt
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,
      variables: { input: { sessionId, page: 1, pageSize: 50 } },
    }),
  ]);

  return {
    session: normalizeSessionDetail(sessionData.session),
    events: eventsData.sessionEvents.items.map(normalizeSessionEvent),
  };
}

export async function appendPrompt(sessionId: string, body: string) {
  return graphqlFetch<
    { appendPrompt: { id: string; sessionId: string; body: string; createdAt: string } },
    { input: { sessionId: string; body: string } }
  >({
    query: `
      mutation AppendPrompt($input: AppendPromptInput!) {
        appendPrompt(input: $input) {
          id
          sessionId
          body
          createdAt
        }
      }
    `,
    variables: { input: { sessionId, body } },
  });
}

export async function stopSession(sessionId: string) {
  return graphqlFetch<{ stopSession: GraphQLSession }, { id: string }>({
    query: `
      mutation StopSession($id: ID!) {
        stopSession(id: $id) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId },
  });
}

export async function createSession(input: CreateSessionInput) {
  try {
    const data = await graphqlFetch<
      { createSession: GraphQLSession },
      { input: CreateSessionInput }
    >({
      query: `
        mutation CreateSession($input: CreateSessionInput!) {
          createSession(input: $input) {
            ${sessionFields}
          }
        }
      `,
      variables: { input },
    });
    return normalizeSession(data.createSession);
  } catch {
    return normalizeSession({
      id: `local-${Date.now()}`,
      projectId: input.projectId,
      requirement: input.requirement,
      mode: input.mode,
      status: 'stopped',
      baseBranch: input.baseBranch ?? 'main',
      config: {
        codexModel: input.config?.codexModel ?? '',
        reasoningEffort: input.config?.reasoningEffort ?? '',
        permissionMode: input.config?.permissionMode ?? '',
      },
      lastRunAt: null,
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    });
  }
}

function normalizeSessionCard(session: GraphQLSessionCard): SessionCard {
  return {
    id: session.id,
    projectId: session.projectId,
    title: session.requirementSummary || firstLine(session.requirement),
    summary: session.requirementSummary || session.requirement,
    mode: normalizeMode(session.mode),
    status: normalizeStatus(session.status),
    branch: session.baseBranch || 'main',
    node: session.currentNodeTitle || statusNode(normalizeStatus(session.status)),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    pendingQuestion: session.pendingQuestion,
    filesChanged: 0,
  };
}

function normalizeSessionDetail(session: GraphQLSessionDetail): SessionDetail {
  const status = normalizeStatus(session.status);
  return {
    id: session.id,
    projectId: session.projectId,
    title: firstLine(session.requirement),
    summary: session.requirement,
    mode: normalizeMode(session.mode),
    status,
    branch: session.baseBranch || 'main',
    node: statusNode(status),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    pendingQuestion: status === 'waiting_user',
    filesChanged: 0,
    config: session.config,
    availableActions: session.availableActions,
    canResume: session.canResume,
  };
}

function normalizeSession(session: GraphQLSession): SessionCard {
  const status = normalizeStatus(session.status);
  return {
    id: session.id,
    projectId: session.projectId,
    title: firstLine(session.requirement),
    summary: session.requirement,
    mode: normalizeMode(session.mode),
    status,
    branch: session.baseBranch || 'main',
    node: statusNode(status),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    pendingQuestion: status === 'waiting_user',
    filesChanged: 0,
  };
}

function normalizeSessionEvent(event: GraphQLSessionEvent): SessionEvent {
  return {
    id: event.id,
    kind: eventKind(event.type),
    title: stringPayload(event.payload, 'title') || eventTitle(event.type),
    body:
      stringPayload(event.payload, 'body') ||
      stringPayload(event.payload, 'message') ||
      JSON.stringify(event.payload),
    time: formatEventTime(event.createdAt),
  };
}

function mockSessionPage(input: ListSessionsInput): SessionPage {
  const page = input.page ?? 1;
  const pageSize = input.pageSize ?? mockSessions.length;
  const start = (page - 1) * pageSize;
  const items = mockSessions.slice(start, start + pageSize);
  return {
    items,
    pageInfo: {
      page,
      pageSize,
      total: mockSessions.length,
      nextCursor: start + pageSize < mockSessions.length ? String(page + 1) : '',
    },
  };
}

function normalizeMode(mode: string): SessionMode {
  return mode === 'chat' ? 'chat' : 'workflow';
}

function normalizeStatus(status: string): SessionStatus {
  const statuses = new Set<SessionStatus>([
    'created',
    'starting',
    'running',
    'waiting_user',
    'stopping',
    'stopped',
    'resume_failed',
    'failed',
    'blocked',
    'completed',
    'closed',
  ]);
  if (statuses.has(status as SessionStatus)) {
    return status as SessionStatus;
  }
  return 'stopped';
}

function eventKind(type: string): SessionEvent['kind'] {
  if (type.includes('tool')) return 'tool';
  if (type.includes('assistant')) return 'assistant';
  if (type.includes('question')) return 'question';
  if (type.includes('thought')) return 'thought';
  return 'status';
}

function eventTitle(type: string) {
  if (type.includes('tool')) return '工具调用';
  if (type.includes('assistant')) return '模型输出';
  if (type.includes('question')) return '待回答';
  if (type.includes('thought')) return '思考';
  return '状态';
}

function statusNode(status: SessionStatus) {
  const labels: Record<SessionStatus, string> = {
    created: '待运行',
    starting: '启动中',
    running: '运行中',
    waiting_user: '待回答',
    stopping: '停止中',
    stopped: '已停止',
    resume_failed: '恢复失败',
    failed: '失败',
    blocked: '阻塞',
    completed: '已完成',
    closed: '已关闭',
  };
  return labels[status];
}

function stringPayload(payload: Record<string, unknown>, key: string) {
  const value = payload[key];
  return typeof value === 'string' ? value : '';
}

function firstLine(value: string) {
  return (
    value
      .split('\n')
      .find((line) => line.trim())
      ?.trim() || '未命名会话'
  );
}

function formatSessionTime(value: string) {
  if (!value) return '';
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}

function formatEventTime(value: string) {
  if (!value) return '';
  return new Intl.DateTimeFormat('zh-CN', {
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value));
}
