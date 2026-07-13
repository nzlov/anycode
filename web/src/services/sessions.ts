import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { sessionStatusLabel } from '@/services/sessionStatusPresentation';
import type { TranscriptEvent } from '@/services/sessionTimeline';

export type SessionMode = 'workflow' | 'chat';
export type SessionStatus =
  | 'created'
  | 'queued'
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
export type SessionPriority = 'high' | 'medium' | 'low';

export interface SessionCard {
  id: string;
  projectId: string;
  projectName: string;
  title: string;
  summary: string;
  mode: SessionMode;
  status: SessionStatus;
  priority: SessionPriority;
  branch: string;
  worktreeBranch: string;
  node: string;
  createdAt: string;
  createdTime: string;
  updatedAt: string;
  updatedTime: string;
  pendingQuestion: boolean;
  todoList?: SessionTodoList | null;
  filesChanged: number;
  availableActions: string[];
}

export interface SessionTodoList {
  completed: number;
  total: number;
  items: SessionTodoItem[];
}

export interface SessionTodoItem {
  text: string;
  completed: boolean;
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
  attachments: SessionAttachment[];
  createdAt: string;
  time: string;
}

export interface SessionAttachment {
  id: string;
  sessionId: string;
  kind: string;
  filename: string;
  mimeType: string;
  size: number;
  previewable: boolean;
  createdAt: string;
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
  events: TranscriptEvent[];
  eventsPageInfo: PageInfo;
}

export interface SessionCardChangedSubscriptionInput {
  projectId?: string;
}

export interface CreateSessionInput {
  projectId: string;
  requirement: string;
  mode: SessionMode;
  priority?: SessionPriority;
  baseBranch?: string;
  config?: {
    codexModel?: string;
    reasoningEffort?: string;
    permissionMode?: string;
  };
  stagedAttachmentIds?: string[];
}

export interface SessionConfigInput {
  codexModel: string;
  reasoningEffort: string;
  permissionMode: string;
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
  priority: string;
  baseBranch: string;
  worktreeBranch: string;
  currentNodeTitle: string;
  pendingQuestion: boolean;
  todoList?: GraphQLSessionTodoList | null;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
  availableActions: string[];
}

interface GraphQLSessionTodoList {
  completed: number;
  total: number;
  items: GraphQLSessionTodoItem[];
}

interface GraphQLSessionTodoItem {
  text: string;
  completed: boolean;
}

interface GraphQLSessionDetail {
  id: string;
  projectId: string;
  requirement: string;
  mode: string;
  status: string;
  priority: string;
  closeReason?: string | null;
  baseBranch: string;
  worktreeBranch: string;
  currentNodeTitle: string;
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  promptAppends?: GraphQLPromptAppend[];
  availableActions?: string[];
  canResume: boolean;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLPromptAppend {
  id: string;
  sessionId: string;
  body: string;
  attachments?: GraphQLSessionAttachment[];
  createdAt: string;
}

interface GraphQLSessionAttachment {
  id: string;
  sessionId: string;
  kind: string;
  filename: string;
  mimeType: string;
  size: number;
  previewable: boolean;
  createdAt: string;
}

interface GraphQLSession {
  id: string;
  projectId: string;
  requirement: string;
  mode: string;
  status: string;
  priority: string;
  baseBranch: string;
  worktreeBranch: string;
  config: {
    codexModel: string;
    reasoningEffort: string;
    permissionMode: string;
  };
  availableActions?: string[];
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLSessionStateStreamItem {
  ready: boolean;
  session?: GraphQLSessionDetail | null;
  questionBatch?: GraphQLQuestionBatch | null;
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
  priority
  baseBranch
  worktreeBranch
  currentNodeTitle
  pendingQuestion
  todoList {
    completed
    total
    items {
      text
      completed
    }
  }
  availableActions
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
  priority
  closeReason
  baseBranch
  worktreeBranch
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
    attachments {
      id
      sessionId
      kind
      filename
      mimeType
      size
      previewable
      createdAt
    }
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
  priority
  baseBranch
  worktreeBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  availableActions
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

export async function getSession(sessionId: string): Promise<SessionDetail> {
  const data = await graphqlFetch<{ session: GraphQLSessionDetail }, { id: string }>({
    query: `
      query SessionProject($id: ID!) {
        session(id: $id) {
          ${sessionDetailFields}
        }
      }
    `,
    variables: { id: sessionId },
  });
  return normalizeSessionDetail(data.session);
}

export function subscribeSessionCardChanged(
  input: SessionCardChangedSubscriptionInput,
  handlers: {
    onData: (card: SessionCard) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
    onSubscribed?: (() => void) | undefined;
  },
) {
  const options = {
    query: `
      subscription SessionCardUpdates($projectId: ID) {
        sessionCardUpdates(projectId: $projectId) {
          ready
          card {
            ${sessionCardFields}
          }
        }
      }
    `,
    variables: input.projectId ? { projectId: input.projectId } : {},
    onData: (data: {
      sessionCardUpdates: { ready: boolean; card?: GraphQLSessionCard | null };
    }) => {
      if (data.sessionCardUpdates.ready) {
        handlers.onSubscribed?.();
        return;
      }
      if (data.sessionCardUpdates.card) {
        handlers.onData(normalizeSessionCard(data.sessionCardUpdates.card));
      }
    },
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<
    { sessionCardUpdates: { ready: boolean; card?: GraphQLSessionCard | null } },
    { projectId?: string }
  >(options);
}

export function subscribeSessionStateUpdates(
  sessionId: string,
  handlers: {
    onData: (update: { session?: SessionDetail; questionBatch?: QuestionBatch }) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
    onSubscribed?: () => void;
  },
) {
  const options = {
    query: `
      subscription SessionStateUpdates($sessionId: ID!) {
        sessionStateUpdates(sessionId: $sessionId) {
          ready
          session {
            ${sessionDetailFields}
          }
          questionBatch {
            ${questionBatchFields}
          }
        }
      }
    `,
    variables: { sessionId },
    onData: (data: { sessionStateUpdates: GraphQLSessionStateStreamItem }) => {
      const item = data.sessionStateUpdates;
      if (item.ready) {
        handlers.onSubscribed?.();
        return;
      }
      const update: { session?: SessionDetail; questionBatch?: QuestionBatch } = {};
      if (item.session) update.session = normalizeSessionDetail(item.session);
      if (item.questionBatch) update.questionBatch = normalizeQuestionBatch(item.questionBatch);
      handlers.onData(update);
    },
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<
    { sessionStateUpdates: GraphQLSessionStateStreamItem },
    { sessionId: string }
  >(options);
}

export async function appendPrompt(
  sessionId: string,
  body: string,
  stagedAttachmentIds?: string[],
) {
  const input: { sessionId: string; body: string; stagedAttachmentIds?: string[] } = {
    sessionId,
    body,
  };
  if (stagedAttachmentIds && stagedAttachmentIds.length > 0) {
    input.stagedAttachmentIds = stagedAttachmentIds;
  }
  return graphqlFetch<
    { appendPrompt: GraphQLPromptAppend },
    { input: { sessionId: string; body: string; stagedAttachmentIds?: string[] } }
  >({
    query: `
      mutation AppendPrompt($input: AppendPromptInput!) {
        appendPrompt(input: $input) {
          id
          sessionId
          body
          attachments {
            id
            sessionId
            kind
            filename
            mimeType
            size
            previewable
            createdAt
          }
          createdAt
        }
      }
    `,
    variables: { input },
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

export async function updateSessionPriority(sessionId: string, priority: SessionPriority) {
  const data = await graphqlFetch<
    { setSessionPriority: GraphQLSession },
    { input: { sessionId: string; priority: SessionPriority } }
  >({
    query: `
      mutation SetSessionPriority($input: SetSessionPriorityInput!) {
        setSessionPriority(input: $input) {
          ${sessionFields}
        }
      }
    `,
    variables: { input: { sessionId, priority } },
  });
  return normalizeSession(data.setSessionPriority);
}

export async function updateSessionConfig(sessionId: string, config: SessionConfigInput) {
  const data = await graphqlFetch<
    { updateSessionConfig: GraphQLSession },
    { input: { sessionId: string; config: SessionConfigInput } }
  >({
    query: `
      mutation UpdateSessionConfig($input: UpdateSessionConfigInput!) {
        updateSessionConfig(input: $input) {
          ${sessionFields}
        }
      }
    `,
    variables: { input: { sessionId, config } },
  });
  return {
    config: data.updateSessionConfig.config,
    updatedAt: formatSessionTime(
      data.updateSessionConfig.lastRunAt ?? data.updateSessionConfig.updatedAt,
    ),
  };
}

export async function executeSession(sessionId: string, force = false) {
  return graphqlFetch<{ executeSession: GraphQLSession }, { id: string; force: boolean }>({
    query: `
      mutation ExecuteSession($id: ID!, $force: Boolean) {
        executeSession(id: $id, force: $force) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId, force },
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
  const data = await graphqlFetch<{ createSession: GraphQLSession }, { input: CreateSessionInput }>(
    {
      query: `
      mutation CreateSession($input: CreateSessionInput!) {
        createSession(input: $input) {
          ${sessionFields}
        }
      }
    `,
      variables: { input },
    },
  );
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

function normalizeTodoList(todoList?: GraphQLSessionTodoList | null): SessionTodoList | null {
  if (!todoList || todoList.total <= 0 || todoList.items.length === 0) {
    return null;
  }
  return {
    completed: todoList.completed,
    total: todoList.total,
    items: todoList.items.map((item) => ({
      text: item.text,
      completed: item.completed,
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
    priority: normalizePriority(session.priority),
    branch: session.baseBranch || 'main',
    worktreeBranch: session.worktreeBranch || '',
    node: session.currentNodeTitle || statusNode(normalizeStatus(session.status)),
    createdAt: session.createdAt,
    createdTime: formatEventTime(session.createdAt),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    updatedTime: session.updatedAt,
    pendingQuestion: session.pendingQuestion,
    todoList: normalizeTodoList(session.todoList),
    filesChanged: 0,
    availableActions: normalizeAvailableActions(session.availableActions),
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
    priority: normalizePriority(session.priority),
    branch: session.baseBranch || 'main',
    worktreeBranch: session.worktreeBranch || '',
    node: session.currentNodeTitle || statusNode(status),
    createdAt: session.createdAt,
    createdTime: formatEventTime(session.createdAt),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    updatedTime: session.updatedAt,
    pendingQuestion: status === 'waiting_user',
    todoList: null,
    filesChanged: 0,
    config: session.config,
    closeReason: session.closeReason ?? null,
    promptAppends: (session.promptAppends ?? []).map(normalizePromptAppend),
    availableActions: normalizeAvailableActions(session.availableActions),
    canResume: session.canResume,
  };
}

function normalizePromptAppend(promptAppend: GraphQLPromptAppend): PromptAppend {
  return {
    id: promptAppend.id,
    sessionId: promptAppend.sessionId,
    body: promptAppend.body,
    attachments: (promptAppend.attachments ?? []).map(normalizeAttachment),
    createdAt: promptAppend.createdAt,
    time: formatEventTime(promptAppend.createdAt),
  };
}

function normalizeAttachment(attachment: GraphQLSessionAttachment): SessionAttachment {
  return {
    id: attachment.id,
    sessionId: attachment.sessionId,
    kind: attachment.kind,
    filename: attachment.filename,
    mimeType: attachment.mimeType,
    size: attachment.size,
    previewable: attachment.previewable,
    createdAt: attachment.createdAt,
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
    priority: normalizePriority(session.priority),
    branch: session.baseBranch || 'main',
    worktreeBranch: session.worktreeBranch || '',
    node: statusNode(status),
    createdAt: session.createdAt,
    createdTime: formatEventTime(session.createdAt),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    updatedTime: session.updatedAt,
    pendingQuestion: status === 'waiting_user',
    todoList: null,
    filesChanged: 0,
    availableActions: normalizeAvailableActions(session.availableActions),
  };
}

function normalizeMode(mode: string): SessionMode {
  return mode === 'chat' ? 'chat' : 'workflow';
}

function normalizeStatus(status: string): SessionStatus {
  const statuses = new Set<SessionStatus>([
    'created',
    'queued',
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

function normalizePriority(priority: string): SessionPriority {
  if (priority === 'high' || priority === 'low') {
    return priority;
  }
  return 'medium';
}

function normalizeAvailableActions(actions: unknown): string[] {
  if (!Array.isArray(actions)) {
    return [];
  }
  return actions.filter((action): action is string => typeof action === 'string');
}

function statusNode(status: SessionStatus) {
  return sessionStatusLabel(status);
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
