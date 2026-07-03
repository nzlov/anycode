import { graphqlFetch, graphqlSubscribe } from '@/services/graphqlClient';

export type SessionMode = 'workflow' | 'chat';
export type SessionStatus =
  | 'created'
  | 'starting'
  | 'running'
  | 'waiting_user'
  | 'waiting_approval'
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
  projectName: string;
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
  closeReason?: string | null;
  promptAppends: PromptAppend[];
  availableActions: string[];
  canResume: boolean;
}

export interface PromptAppend {
  id: string;
  sessionId: string;
  body: string;
  createdAt: string;
  time: string;
}

export interface SessionEvent {
  id: string;
  kind: 'thought' | 'tool' | 'assistant' | 'status' | 'question';
  rawType: string;
  title: string;
  body: string;
  time: string;
}

export interface QuestionOption {
  id: string;
  label: string;
  description: string;
  payload: Record<string, unknown>;
}

export interface AgentQuestion {
  id: string;
  batchId: string;
  title: string;
  body: string;
  type: string;
  options: QuestionOption[];
  allowCustom: boolean;
  selectedOptionId?: string | null;
  customAnswer: string;
  answer: Record<string, unknown>;
  status: string;
}

export interface QuestionBatch {
  id: string;
  sessionId: string;
  status: string;
  questions: AgentQuestion[];
}

export interface QuestionAnswerInput {
  questionId: string;
  selectedOptionId?: string | null;
  customAnswer?: string;
  payload?: Record<string, unknown>;
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

export interface SessionEventsSubscriptionInput {
  sessionId?: string;
  projectId?: string;
  afterEventId?: string;
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
  closeReason?: string | null;
  baseBranch: string;
  currentNodeTitle: string;
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  promptAppends: GraphQLPromptAppend[];
  availableActions: string[];
  canResume: boolean;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLPromptAppend {
  id: string;
  sessionId: string;
  body: string;
  createdAt: string;
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

interface GraphQLQuestionBatch {
  id: string;
  sessionId: string;
  status: string;
  questions: AgentQuestion[];
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
  closeReason
  baseBranch
  currentNodeTitle
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  promptAppends {
    id
    sessionId
    body
    createdAt
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

const questionBatchFields = `
  id
  sessionId
  status
  questions {
    id
    batchId
    title
    body
    type
    options {
      id
      label
      description
      payload
    }
    allowCustom
    selectedOptionId
    customAnswer
    answer
    status
  }
`;

export async function listSessions(input: ListSessionsInput = {}): Promise<SessionPage> {
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

export function subscribeSessionEvents(
  input: SessionEventsSubscriptionInput,
  handlers: {
    onData: (event: SessionEvent) => void;
    onError?: (error: Error) => void;
    onClose?: () => void;
  },
) {
  const options = {
    query: `
      subscription SessionEvents($input: SessionEventsInput!) {
        sessionEvents(input: $input) {
          id
          type
          payload
          createdAt
        }
      }
    `,
    variables: { input },
    onData: (data: { sessionEvents: GraphQLSessionEvent }) =>
      handlers.onData(normalizeSessionEvent(data.sessionEvents)),
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<
    { sessionEvents: GraphQLSessionEvent },
    { input: SessionEventsSubscriptionInput }
  >(options);
}

export function subscribePendingQuestionBatches(
  sessionId: string,
  handlers: {
    onData: (batch: QuestionBatch) => void;
    onError?: (error: Error) => void;
    onClose?: () => void;
  },
) {
  const options = {
    query: `
      subscription PendingQuestionBatches($sessionId: ID!) {
        pendingQuestionBatches(sessionId: $sessionId) {
          ${questionBatchFields}
        }
      }
    `,
    variables: { sessionId },
    onData: (data: { pendingQuestionBatches: GraphQLQuestionBatch }) =>
      handlers.onData(normalizeQuestionBatch(data.pendingQuestionBatches)),
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<{ pendingQuestionBatches: GraphQLQuestionBatch }, { sessionId: string }>(
    options,
  );
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

export async function closeSession(sessionId: string) {
  return graphqlFetch<
    { closeSession: GraphQLSession },
    { input: { sessionId: string; reason: 'user_closed' } }
  >({
    query: `
      mutation CloseSession($input: CloseSessionInput!) {
        closeSession(input: $input) {
          ${sessionFields}
        }
      }
    `,
    variables: { input: { sessionId, reason: 'user_closed' } },
  });
}

export async function startSession(sessionId: string) {
  return graphqlFetch<{ startSession: GraphQLSession }, { id: string }>({
    query: `
      mutation StartSession($id: ID!) {
        startSession(id: $id) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId },
  });
}

export async function resumeSession(sessionId: string) {
  return graphqlFetch<{ resumeSession: GraphQLSession }, { id: string }>({
    query: `
      mutation ResumeSession($id: ID!) {
        resumeSession(id: $id) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId },
  });
}

export async function getPendingQuestionBatches(sessionId: string): Promise<QuestionBatch[]> {
  const data = await graphqlFetch<
    { pendingQuestionBatches: GraphQLQuestionBatch[] },
    { sessionId: string }
  >({
    query: `
      query PendingQuestionBatches($sessionId: ID!) {
        pendingQuestionBatches(sessionId: $sessionId) {
          ${questionBatchFields}
        }
      }
    `,
    variables: { sessionId },
  });
  return data.pendingQuestionBatches.map(normalizeQuestionBatch);
}

export async function submitQuestionBatch(batchId: string, answers: QuestionAnswerInput[]) {
  const data = await graphqlFetch<
    { submitQuestionBatch: GraphQLQuestionBatch },
    { input: { batchId: string; answers: QuestionAnswerInput[] } }
  >({
    query: `
      mutation SubmitQuestionBatch($input: SubmitQuestionBatchInput!) {
        submitQuestionBatch(input: $input) {
          ${questionBatchFields}
        }
      }
    `,
    variables: { input: { batchId, answers } },
  });
  return normalizeQuestionBatch(data.submitQuestionBatch);
}

export async function createSession(input: CreateSessionInput) {
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
}

function normalizeQuestionBatch(batch: GraphQLQuestionBatch): QuestionBatch {
  return {
    id: batch.id,
    sessionId: batch.sessionId,
    status: batch.status,
    questions: batch.questions.map((question) => ({
      ...question,
      options: question.options.map((option) => ({
        ...option,
        payload: option.payload ?? {},
      })),
      answer: question.answer ?? {},
    })),
  };
}

function normalizeSessionCard(session: GraphQLSessionCard): SessionCard {
  return {
    id: session.id,
    projectId: session.projectId,
    projectName: session.projectName || session.projectId,
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
    projectName: session.projectId,
    title: firstLine(session.requirement),
    summary: session.requirement,
    mode: normalizeMode(session.mode),
    status,
    branch: session.baseBranch || 'main',
    node: session.currentNodeTitle || statusNode(status),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    pendingQuestion: status === 'waiting_user',
    filesChanged: 0,
    config: session.config,
    closeReason: session.closeReason ?? null,
    promptAppends: session.promptAppends.map(normalizePromptAppend),
    availableActions: session.availableActions,
    canResume: session.canResume,
  };
}

function normalizePromptAppend(promptAppend: GraphQLPromptAppend): PromptAppend {
  return {
    id: promptAppend.id,
    sessionId: promptAppend.sessionId,
    body: promptAppend.body,
    createdAt: promptAppend.createdAt,
    time: formatEventTime(promptAppend.createdAt),
  };
}

function normalizeSession(session: GraphQLSession): SessionCard {
  const status = normalizeStatus(session.status);
  return {
    id: session.id,
    projectId: session.projectId,
    projectName: session.projectId,
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
    rawType: event.type,
    title: stringPayload(event.payload, 'title') || eventTitle(event.type),
    body:
      stringPayload(event.payload, 'body') ||
      stringPayload(event.payload, 'message') ||
      JSON.stringify(event.payload),
    time: formatEventTime(event.createdAt),
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
    'waiting_approval',
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
    waiting_approval: '待审批',
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
