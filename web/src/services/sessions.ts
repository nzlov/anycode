import { graphqlFetch, graphqlSubscribe } from '@/services/graphqlClient';
import { latestSessionEventPageInput } from '@/services/sessionEventPaging';
import { codexCommandResultBody } from '@/services/sessionEventPresentation';

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

export interface SessionEvent {
  id: string;
  kind: 'thought' | 'tool' | 'assistant' | 'status' | 'question' | 'file_change';
  rawType: string;
  title: string;
  body: string;
  command?: string;
  toolCallId?: string;
  fileChangeId?: string;
  fileChanges?: FileChange[];
  createdAt: string;
  time: string;
}

export interface FileChange {
  kind: string;
  path: string;
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
  eventsPageInfo: PageInfo;
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

interface GraphQLSessionEvent {
  id: string;
  type?: string;
  payload?: Record<string, unknown>;
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

export async function getSessionDetail(sessionId: string): Promise<SessionDetailData> {
  const eventPageSize = 50;
  const [eventsData, sessionData] = await Promise.all([
    getSessionEventPage(sessionId, '', eventPageSize),
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
  ]);

  return {
    session: normalizeSessionDetail(sessionData.session),
    events: eventsData.items,
    eventsPageInfo: eventsData.pageInfo,
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

export async function getSessionEventPage(sessionId: string, beforeEventId: string, limit: number) {
  const data = await graphqlFetch<
    { sessionEvents: { items: GraphQLSessionEvent[]; pageInfo: GraphQLPageInfo } },
    { input: { sessionId: string; beforeEventId?: string; limit: number } }
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
    variables: { input: latestSessionEventPageInput(sessionId, beforeEventId, limit) },
  });
  return {
    items: data.sessionEvents.items.map(normalizeSessionEvent),
    pageInfo: data.sessionEvents.pageInfo,
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

export async function appendPrompt(sessionId: string, body: string, stagedAttachmentIds?: string[]) {
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
    updatedAt: formatSessionTime(data.updateSessionConfig.lastRunAt ?? data.updateSessionConfig.updatedAt),
  };
}

export async function startSession(sessionId: string, force = false) {
  return graphqlFetch<{ startSession: GraphQLSession }, { id: string; force: boolean }>({
    query: `
      mutation StartSession($id: ID!, $force: Boolean) {
        startSession(id: $id, force: $force) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId, force },
  });
}

export async function resumeSession(sessionId: string, force = false) {
  return graphqlFetch<{ resumeSession: GraphQLSession }, { id: string; force: boolean }>({
    query: `
      mutation ResumeSession($id: ID!, $force: Boolean) {
        resumeSession(id: $id, force: $force) {
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

export function normalizeSessionEvent(event: GraphQLSessionEvent): SessionEvent {
  const type = event.type ?? '';
  const payload = event.payload ?? {};
  const readable = readableEventPayload(type, payload);
  const normalized: SessionEvent = {
    id: event.id,
    kind: eventKind(type, payload),
    rawType: type,
    title: readable.title || stringPayload(payload, 'title') || eventTitle(type),
    body: readable.body || stringPayload(payload, 'body') || stringPayload(payload, 'message'),
    createdAt: event.createdAt,
    time: formatEventTime(event.createdAt),
  };
  if (readable.command) {
    normalized.command = readable.command;
  }
  if (readable.toolCallId) {
    normalized.toolCallId = readable.toolCallId;
  }
  if (readable.fileChangeId) {
    normalized.fileChangeId = readable.fileChangeId;
  }
  if (readable.fileChanges) {
    normalized.fileChanges = readable.fileChanges;
  }
  return normalized;
}

function readableEventPayload(type: string, payload: Record<string, unknown>) {
  if (type === 'process.codex_event') {
    return readableCodexEvent(payload);
  }

  const status = stringPayload(payload, 'status');
  const reason = stringPayload(payload, 'reason');
  const blockedReason = stringPayload(payload, 'blockedReason');
  const exitCode = numberPayload(payload, 'exitCode');
  const failure = stringPayload(payload, 'failureReason');
  const queueKind = stringPayload(payload, 'queueKind');
  const priority = stringPayload(payload, 'priority');

  if (type === 'session.queued') {
    return {
      title: '排队中',
      body: [
        queueKind === 'answer_user' ? '等待用户回答后的继续执行。' : '等待可用执行槽。',
        priority ? `队列优先级：${statusText(priority)}` : '',
      ]
        .filter(Boolean)
        .join('\n'),
    };
  }
  if (type === 'session.starting') return { title: '启动中', body: '正在启动 Codex 进程。' };
  if (type === 'session.waiting_user') return { title: '待回答', body: 'Codex 正在等待用户回答。' };
  if (type === 'session.running') {
    return { title: '运行中', body: reason ? statusText(reason) : '会话正在运行。' };
  }
  if (type === 'session.stopped') return { title: '已停止', body: failure || '会话已停止。' };
  if (type === 'session.stopping') return { title: '停止中', body: '正在停止 Codex 进程。' };
  if (type === 'session.started') return { title: '已启动', body: 'Codex 进程已启动。' };
  if (type === 'session.failed')
    return { title: '失败', body: failure || reason || '会话执行失败。' };
  if (type === 'session.resume_failed')
    return { title: '恢复失败', body: failure || reason || '恢复 Codex 会话失败。' };
  if (type === 'session.completed') return { title: '已完成', body: '会话已完成。' };
  if (type === 'process.exited') {
    const body =
      exitCode === null
        ? failure || 'Codex 进程已退出。'
        : exitCode === 0 && !failure
          ? ''
          : `退出码 ${exitCode}${failure ? `，${failure}` : ''}`;
    return {
      title: '进程退出',
      body,
    };
  }
  if (type.startsWith('workflow.')) {
    return { title: workflowEventTitle(type), body: blockedReason || statusText(status) };
  }
  if (status) return { title: eventTitle(type), body: statusText(status) };

  return { title: '', body: compactPayload(payload) };
}

function readableCodexEvent(payload: Record<string, unknown>) {
  const codexType = stringPayload(payload, 'codexType');
  const status = stringPayload(payload, 'status');
  const item = objectPayload(payload, 'item');
  const itemType = stringPayload(item, 'type');
  const output = stringPayload(item, 'aggregated_output');
  const text = stringPayload(item, 'text') || stringPayload(payload, 'text');
  const command = stringPayload(item, 'command');
  const toolCallId = codexItemID(item, payload);
  const processExitCode = numberPayload(payload, 'exitCode');
  const failure = stringPayload(payload, 'failureReason');

  if (codexType === 'thread.started') {
    const threadID =
      stringPayload(payload, 'thread_id') || stringPayload(parseRaw(payload), 'thread_id');
    return { title: '线程已创建', body: threadID ? `线程 ${threadID}` : statusText(status) };
  }
  if (codexType === 'turn.started') return { title: '开始执行', body: 'Codex 开始处理当前请求。' };
  if (codexType === 'item.started') {
    if (itemType === 'command_execution') {
      return { title: '执行命令', body: command || 'Codex 正在执行命令。', command, toolCallId };
    }
    if (itemType === 'file_change') {
      const fileChanges = fileChangesFromItem(item);
      return {
        title: fileChangeTitle(fileChanges),
        body: fileChangeBody(fileChanges),
        fileChangeId: toolCallId,
        fileChanges,
      };
    }
    if (itemType === 'agent_message') {
      return { title: '模型输出', body: text || 'Codex 正在生成回复。' };
    }
    return {
      title: itemEventTitle(itemType, '开始'),
      body: compactPayload(item) || statusText(status),
    };
  }
  if (codexType === 'item.completed') {
    if (itemType === 'command_execution') {
      return { title: '命令结果', body: codexCommandResultBody(item), command, toolCallId };
    }
    if (itemType === 'file_change') {
      const fileChanges = fileChangesFromItem(item);
      return {
        title: fileChangeTitle(fileChanges),
        body: fileChangeBody(fileChanges),
        fileChangeId: toolCallId,
        fileChanges,
      };
    }
    if (itemType === 'agent_message') {
      return { title: '模型输出', body: output || text || compactPayload(item) };
    }
    return {
      title: itemEventTitle(itemType, '完成'),
      body: output || compactPayload(item) || statusText(status),
    };
  }
  if (codexType === 'turn.completed') return { title: '本轮完成', body: 'Codex 已完成本轮处理。' };
  if (codexType === 'process.exit') {
    const body =
      processExitCode === null
        ? failure || 'Codex 进程已退出。'
        : processExitCode === 0 && !failure
          ? ''
          : `退出码 ${processExitCode}${failure ? `，${failure}` : ''}`;
    return {
      title: '进程退出',
      body,
    };
  }
  if (codexType === 'error') {
    return {
      title: 'Codex 错误',
      body: stringPayload(payload, 'message') || compactPayload(payload),
    };
  }

  return {
    title: codexType ? codexType.replaceAll('.', ' ') : 'Codex 事件',
    body: compactPayload(payload),
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

function eventKind(type: string, payload: Record<string, unknown> = {}): SessionEvent['kind'] {
  if (type === 'process.codex_event') {
    const codexType = stringPayload(payload, 'codexType');
    const item = objectPayload(payload, 'item');
    const itemType = stringPayload(item, 'type');
    if (itemType === 'command_execution') return 'tool';
    if (itemType === 'file_change') return 'file_change';
    if (itemType === 'agent_message') return 'assistant';
    if (codexType === 'error') return 'status';
  }
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
    queued: '排队中',
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

function workflowEventTitle(type: string) {
  if (type.includes('waiting_approval')) return '等待审批';
  if (type.includes('started')) return '流程开始';
  if (type.includes('completed')) return '流程完成';
  if (type.includes('blocked')) return '流程阻塞';
  if (type.includes('failed')) return '流程失败';
  return '流程事件';
}

function itemEventTitle(type: string, suffix: string) {
  if (type === 'message') return `模型输出${suffix}`;
  if (type === 'reasoning') return `思考${suffix}`;
  if (type === 'tool_call') return `工具调用${suffix}`;
  if (type === 'command_execution') return `命令${suffix}`;
  return `事件${suffix}`;
}

function statusText(value: string) {
  const labels: Record<string, string> = {
    created: '已创建',
    starting: '启动中',
    running: '运行中',
    waiting_user: '等待用户回答',
    waiting_approval: '等待人工审批',
    stopping: '停止中',
    stopped: '已停止',
    completed: '已完成',
    failed: '失败',
    blocked: '阻塞',
    user_answered: '用户已回答，继续运行。',
    immediate: '最高',
    high: '高',
    medium: '中',
    low: '低',
    start: '启动',
    resume: '恢复',
    answer_user: '回答后继续',
  };
  return labels[value] ?? value;
}

function stringPayload(payload: Record<string, unknown>, key: string) {
  const value = payload[key];
  return typeof value === 'string' ? value : '';
}

function numberPayload(payload: Record<string, unknown>, key: string) {
  const value = payload[key];
  return typeof value === 'number' ? value : null;
}

function objectPayload(payload: Record<string, unknown>, key: string): Record<string, unknown> {
  const value = payload[key];
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function codexItemID(item: Record<string, unknown>, payload: Record<string, unknown>) {
  return (
    stringPayload(item, 'id') ||
    stringPayload(item, 'item_id') ||
    stringPayload(item, 'itemId') ||
    stringPayload(item, 'call_id') ||
    stringPayload(item, 'callId') ||
    stringPayload(payload, 'id') ||
    stringPayload(payload, 'item_id') ||
    stringPayload(payload, 'itemId') ||
    stringPayload(payload, 'call_id') ||
    stringPayload(payload, 'callId')
  );
}

function fileChangesFromItem(item: Record<string, unknown>): FileChange[] {
  const changes = item.changes;
  if (!Array.isArray(changes)) return [];
  return changes
    .map((change) => {
      if (!change || typeof change !== 'object' || Array.isArray(change)) return null;
      const entry = change as Record<string, unknown>;
      const path = stringPayload(entry, 'path');
      if (!path) return null;
      return {
        kind: stringPayload(entry, 'kind') || 'update',
        path,
      };
    })
    .filter((change): change is FileChange => change !== null);
}

function fileChangeTitle(changes: FileChange[]) {
  if (changes.length === 0) return '修改文件';
  const [firstChange] = changes;
  if (changes.length === 1 && firstChange) return `修改文件 ${firstChange.path}`;
  const visiblePaths = changes.slice(0, 3).map((change) => change.path);
  const suffix = changes.length > visiblePaths.length ? ` 等 ${changes.length} 个文件` : '';
  return `修改文件 ${visiblePaths.join(', ')}${suffix}`;
}

function fileChangeBody(changes: FileChange[]) {
  return changes.map((change) => `${fileChangeKindText(change.kind)} ${change.path}`).join('\n');
}

function fileChangeKindText(kind: string) {
  const labels: Record<string, string> = {
    add: '新增',
    create: '新增',
    delete: '删除',
    remove: '删除',
    update: '修改',
    modify: '修改',
    rename: '重命名',
  };
  return labels[kind] ?? kind;
}

function parseRaw(payload: Record<string, unknown>) {
  const raw = stringPayload(payload, 'raw');
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw) as unknown;
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {};
  } catch {
    return {};
  }
}

function compactPayload(payload: Record<string, unknown>) {
  const parts = Object.entries(payload)
    .filter(([key]) => !['raw', 'processRunId', 'codexEventId'].includes(key))
    .map(([key, value]) => {
      if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
        return `${key}: ${value}`;
      }
      return '';
    })
    .filter(Boolean);
  return parts.join(' · ');
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
