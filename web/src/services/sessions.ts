import {
  graphqlFetch,
  graphqlSubscribe,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { sessionStatusLabel } from '@/services/sessionStatusPresentation';
import type { SessionFile } from '@/services/sessionFiles';
import {
  normalizeTranscriptEvent,
  transcriptEventFields,
  transcriptUsageFields,
  type GraphQLTranscriptEvent,
  type TranscriptEvent,
  type TranscriptTokenUsage,
} from '@/services/sessionTimeline';
import { normalizeWorkflowNodeResult } from '@/services/workflowApprovalReview';

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
export type WorktreeCleanupStatus =
  'not_applicable' | 'provisioning' | 'active' | 'pending' | 'failed' | 'cleaned';

export interface WorktreeCleanup {
  status: WorktreeCleanupStatus;
  attempts: number;
  requestedAt?: string | null;
  completedAt?: string | null;
  error?: {
    code: string;
    message: string;
    retryable: boolean;
  } | null;
}

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
  todoList?: SessionTodoList | null;
  artifactCount: number;
  filesChanged: number;
  usage?: TranscriptTokenUsage | null;
  availableActions: string[];
}

export interface PendingApproval {
  sessionId: string;
  nodeId: string;
  nodeRunId: string | null;
  currentNodeTitle: string;
  phase: 'before_run' | 'after_run';
  result: WorkflowNodeResult | null;
}

export interface WorkflowNodeResult {
  version: 1;
  outcome: 'success' | 'partial' | 'failure';
  summary: string;
  data: Record<string, unknown>;
  checks: Array<{
    id: string;
    label: string;
    status: 'passed' | 'warning' | 'failed';
    detail?: string;
    source: 'agent' | 'system';
  }>;
  warnings: Array<{ code: string; message: string }>;
  artifacts: Array<{ kind: string; label: string; ref: string }>;
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
  codexSessionId: string;
  pendingApproval?: PendingApproval | null;
  config: SessionConfig;
  closeReason?: string | null;
  promptAppends: PromptAppend[];
  availableActions: string[];
  canResume: boolean;
  worktreeCleanup: WorktreeCleanup;
}

export interface PromptAppend {
  id: string;
  sessionId: string;
  body: string;
  attachments: SessionAttachment[];
  artifacts: SessionFile[];
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

export interface SubmitWorkflowApprovalInput {
  sessionId: string;
  nodeId: string;
  approved: boolean;
  comment?: string;
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
  olderThanDays?: number;
  page?: number;
  pageSize?: number;
  filter?: string;
  sort?: string;
}

export interface CleanupSessionsInput {
  projectId?: string;
  scope?: string;
  filter?: string;
  olderThanDays: number;
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
    fastMode?: boolean;
  };
  stagedAttachmentIds?: string[];
}

export interface SessionConfig {
  codexModel: string;
  reasoningEffort: string;
  permissionMode: string;
  fastMode: boolean;
}

export type SessionConfigInput = SessionConfig;

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
  todoList?: GraphQLSessionTodoList | null;
  artifactCount: number;
  filesChanged: number;
  usage?: TranscriptTokenUsage | null;
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
  availableActions: string[];
}

interface GraphQLPendingApproval {
  sessionId: string;
  nodeId: string;
  nodeRunId: string | null;
  currentNodeTitle: string;
  phase: 'before_run' | 'after_run';
  result: unknown;
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
  projectName: string;
  requirement: string;
  mode: string;
  status: string;
  priority: string;
  closeReason?: string | null;
  baseBranch: string;
  worktreeBranch: string;
  codexSessionId: string;
  worktreeCleanup: WorktreeCleanup;
  currentNodeTitle: string;
  pendingApproval?: GraphQLPendingApproval | null;
  todoList?: GraphQLSessionTodoList | null;
  config: SessionConfig;
  usage?: TranscriptTokenUsage | null;
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
  artifacts?: SessionFile[];
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
  worktreeCleanup: WorktreeCleanup;
  config: SessionConfig;
  availableActions?: string[];
  lastRunAt: string | null;
  createdAt: string;
  updatedAt: string;
}

interface GraphQLSessionStatusUpdate {
  status: string;
  currentNodeTitle: string;
  availableActions: string[];
  updatedAt: string;
}

interface GraphQLSessionUpdateEvent {
  eventType: string;
  sessionId: string;
  occurredAt?: string | null;
  status?: GraphQLSessionStatusUpdate | null;
  todoList?: GraphQLSessionTodoList | null;
  usage?: TranscriptTokenUsage | null;
  filesChanged?: number | null;
  artifactCount?: number | null;
  priority?: string | null;
  config?: SessionConfig | null;
  worktreeCleanup?: WorktreeCleanup | null;
  availableActions?: string[] | null;
  updatedAt?: string | null;
}

export interface SessionStatusUpdate {
  status: SessionStatus;
  node: string;
  availableActions: string[];
  updatedAt: string;
  updatedTime: string;
}

export interface SessionUpdateEvent {
  eventType: string;
  sessionId: string;
  occurredAt?: string | null;
  status?: SessionStatusUpdate;
  todoList?: SessionTodoList | null;
  usage?: TranscriptTokenUsage;
  filesChanged?: number;
  artifactCount?: number;
  priority?: SessionPriority;
  config?: SessionConfig;
  worktreeCleanup?: WorktreeCleanup;
  availableActions?: string[];
  updatedAt?: string;
  updatedTime?: string;
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
  todoList {
    completed
    total
    items {
      text
      completed
    }
  }
  artifactCount
  filesChanged
  usage { ${transcriptUsageFields} }
  availableActions
  lastRunAt
  createdAt
  updatedAt
`;

const sessionDetailFields = `
  id
  projectId
  projectName
  requirement
  mode
  status
  priority
  closeReason
  baseBranch
  worktreeBranch
  codexSessionId
  worktreeCleanup {
    status
    attempts
    requestedAt
    completedAt
    error {
      code
      message
      retryable
    }
  }
  currentNodeTitle
  pendingApproval {
    sessionId
    nodeId
    nodeRunId
    currentNodeTitle
    phase
    result
  }
  todoList {
    completed
    total
    items {
      text
      completed
    }
  }
  config {
    codexModel
    reasoningEffort
    permissionMode
    fastMode
  }
  usage { ${transcriptUsageFields} }
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
    artifacts {
      id sessionId role sourceType artifactKind logicalPath filename mimeType size
      previewKind previewUrl downloadUrl createdAt
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
  worktreeCleanup {
    status
    attempts
    requestedAt
    completedAt
    error {
      code
      message
      retryable
    }
  }
  config {
    codexModel
    reasoningEffort
    permissionMode
    fastMode
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

export async function getSessionCard(
  sessionId: string,
  options: { notify?: boolean } = {},
): Promise<SessionCard> {
  const data = await graphqlFetch<{ sessionCard: GraphQLSessionCard }, { id: string }>({
    query: `
      query SessionCard($id: ID!) {
        sessionCard(id: $id) {
          ${sessionCardFields}
        }
      }
    `,
    variables: { id: sessionId },
    notify: options.notify ?? true,
  });
  return normalizeSessionCard(data.sessionCard);
}

export function subscribeSessionUpdates(handlers: {
  onData: (update: SessionUpdateEvent) => void;
  onError?: (error: Error) => void;
  onClose?: (close: GraphQLSubscriptionClose) => void;
}) {
  const options = {
    query: `
      subscription SessionUpdates {
        sessionUpdates {
          eventType
          sessionId
          occurredAt
          status {
            status
            currentNodeTitle
            availableActions
            updatedAt
          }
          todoList {
            completed
            total
            items {
              text
              completed
            }
          }
          usage { ${transcriptUsageFields} }
          filesChanged
          artifactCount
          priority
          config {
            codexModel
            reasoningEffort
            permissionMode
            fastMode
          }
          worktreeCleanup {
            status
            attempts
            requestedAt
            completedAt
            error {
              code
              message
              retryable
            }
          }
          availableActions
          updatedAt
        }
      }
    `,
    onData: (data: { sessionUpdates: GraphQLSessionUpdateEvent }) =>
      handlers.onData(normalizeSessionUpdate(data.sessionUpdates)),
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<{ sessionUpdates: GraphQLSessionUpdateEvent }>(options);
}

export function subscribeSessionEvents(
  sessionId: string,
  handlers: {
    onData: (event: TranscriptEvent) => void;
    onError?: (error: Error) => void;
    onClose?: (close: GraphQLSubscriptionClose) => void;
  },
) {
  const options = {
    query: `
      subscription SessionEvents($sessionId: ID!) {
        sessionEvents(sessionId: $sessionId) {
          ${transcriptEventFields}
        }
      }
    `,
    variables: { sessionId },
    onData: (data: { sessionEvents: GraphQLTranscriptEvent }) =>
      handlers.onData(normalizeTranscriptEvent(data.sessionEvents)),
  };
  if (handlers.onError) {
    Object.assign(options, { onError: handlers.onError });
  }
  if (handlers.onClose) {
    Object.assign(options, { onClose: handlers.onClose });
  }
  return graphqlSubscribe<{ sessionEvents: GraphQLTranscriptEvent }, { sessionId: string }>(
    options,
  );
}

export async function appendPrompt(
  sessionId: string,
  body: string,
  stagedAttachmentIds?: string[],
  artifactIds?: string[],
) {
  const input: {
    sessionId: string;
    body: string;
    stagedAttachmentIds?: string[];
    artifactIds?: string[];
  } = {
    sessionId,
    body,
  };
  if (stagedAttachmentIds && stagedAttachmentIds.length > 0) {
    input.stagedAttachmentIds = stagedAttachmentIds;
  }
  if (artifactIds && artifactIds.length > 0) input.artifactIds = artifactIds;
  return graphqlFetch<
    { appendPrompt: GraphQLPromptAppend },
    {
      input: {
        sessionId: string;
        body: string;
        stagedAttachmentIds?: string[];
        artifactIds?: string[];
      };
    }
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
          artifacts {
            id sessionId role sourceType artifactKind logicalPath filename mimeType size
            previewKind previewUrl downloadUrl createdAt
          }
          createdAt
        }
      }
    `,
    variables: { input },
  });
}

export async function updatePromptAppend(
  sessionId: string,
  promptAppendId: string,
  body: string,
): Promise<PromptAppend> {
  const data = await graphqlFetch<
    { updatePromptAppend: GraphQLPromptAppend },
    { input: { sessionId: string; promptAppendId: string; body: string } }
  >({
    query: `
      mutation UpdatePromptAppend($input: UpdatePromptAppendInput!) {
        updatePromptAppend(input: $input) {
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
          artifacts {
            id sessionId role sourceType artifactKind logicalPath filename mimeType size
            previewKind previewUrl downloadUrl createdAt
          }
          createdAt
        }
      }
    `,
    variables: { input: { sessionId, promptAppendId, body } },
    notify: false,
  });
  return normalizePromptAppend(data.updatePromptAppend);
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

export async function cleanupSessions(input: CleanupSessionsInput): Promise<number> {
  const data = await graphqlFetch<{ cleanupSessions: number }, { input: CleanupSessionsInput }>({
    query: `
      mutation CleanupSessions($input: CleanupSessionsInput!) {
        cleanupSessions(input: $input)
      }
    `,
    variables: { input },
  });
  return data.cleanupSessions;
}

export async function retrySessionWorktreeCleanup(sessionId: string) {
  const data = await graphqlFetch<{ retrySessionWorktreeCleanup: GraphQLSession }, { id: string }>({
    query: `
      mutation RetrySessionWorktreeCleanup($id: ID!) {
        retrySessionWorktreeCleanup(id: $id) {
          ${sessionFields}
        }
      }
    `,
    variables: { id: sessionId },
  });
  return normalizeSession(data.retrySessionWorktreeCleanup);
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

export async function submitWorkflowApproval(input: SubmitWorkflowApprovalInput) {
  const data = await graphqlFetch<
    {
      submitWorkflowApproval: {
        sessionId: string;
        status: string;
        currentNodeId: string;
        context: Record<string, unknown>;
      };
    },
    { input: SubmitWorkflowApprovalInput }
  >({
    query: `
      mutation SubmitWorkflowApproval($input: SubmitWorkflowApprovalInput!) {
        submitWorkflowApproval(input: $input) {
          sessionId
          status
          currentNodeId
          context
        }
      }
    `,
    variables: { input },
  });
  return data.submitWorkflowApproval;
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

function normalizePendingApproval(
  approval?: GraphQLPendingApproval | null,
): PendingApproval | null {
  if (!approval) return null;
  const result = normalizeWorkflowNodeResult(approval.result);
  return {
    sessionId: approval.sessionId,
    nodeId: approval.nodeId,
    nodeRunId: approval.nodeRunId,
    currentNodeTitle: approval.currentNodeTitle,
    phase: approval.phase,
    result,
  };
}

function normalizeSessionUpdate(item: GraphQLSessionUpdateEvent): SessionUpdateEvent {
  const update: SessionUpdateEvent = {
    eventType: item.eventType,
    sessionId: item.sessionId,
  };
  if (item.occurredAt !== undefined) update.occurredAt = item.occurredAt;
  if (item.status) {
    const status = normalizeStatus(item.status.status);
    update.status = {
      status,
      node: item.status.currentNodeTitle || statusNode(status),
      availableActions: normalizeAvailableActions(item.status.availableActions),
      updatedAt: formatSessionTime(item.status.updatedAt),
      updatedTime: item.status.updatedAt,
    };
  }
  if (item.eventType === 'session.todo_list_updated') {
    update.todoList = normalizeTodoList(item.todoList);
  }
  if (item.usage) update.usage = item.usage;
  if (typeof item.filesChanged === 'number') {
    update.filesChanged = Math.max(0, item.filesChanged);
  }
  if (typeof item.artifactCount === 'number') {
    update.artifactCount = Math.max(0, item.artifactCount);
  }
  if (item.priority) update.priority = normalizePriority(item.priority);
  if (item.config) update.config = item.config;
  if (item.worktreeCleanup) update.worktreeCleanup = item.worktreeCleanup;
  if (item.availableActions) {
    update.availableActions = normalizeAvailableActions(item.availableActions);
  }
  if (item.updatedAt) {
    update.updatedAt = formatSessionTime(item.updatedAt);
    update.updatedTime = item.updatedAt;
  }
  return update;
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
    todoList: normalizeTodoList(session.todoList),
    artifactCount: Math.max(0, session.artifactCount),
    filesChanged: Math.max(0, session.filesChanged),
    usage: session.usage ?? null,
    availableActions: normalizeAvailableActions(session.availableActions),
  };
}

function normalizeSessionDetail(session: GraphQLSessionDetail): SessionDetail {
  const status = normalizeStatus(session.status);
  return {
    id: session.id,
    projectId: session.projectId,
    projectName: session.projectName,
    title: firstLine(session.requirement),
    summary: session.requirement,
    mode: normalizeMode(session.mode),
    status,
    priority: normalizePriority(session.priority),
    branch: session.baseBranch || 'main',
    worktreeBranch: session.worktreeBranch || '',
    codexSessionId: session.codexSessionId || '',
    node: session.currentNodeTitle || statusNode(status),
    createdAt: session.createdAt,
    createdTime: formatEventTime(session.createdAt),
    updatedAt: formatSessionTime(session.lastRunAt ?? session.updatedAt),
    updatedTime: session.updatedAt,
    pendingApproval: normalizePendingApproval(session.pendingApproval),
    todoList: normalizeTodoList(session.todoList),
    artifactCount: 0,
    filesChanged: 0,
    usage: session.usage ?? null,
    config: session.config,
    closeReason: session.closeReason ?? null,
    promptAppends: (session.promptAppends ?? []).map(normalizePromptAppend),
    availableActions: normalizeAvailableActions(session.availableActions),
    canResume: session.canResume,
    worktreeCleanup: session.worktreeCleanup,
  };
}

function normalizePromptAppend(promptAppend: GraphQLPromptAppend): PromptAppend {
  return {
    id: promptAppend.id,
    sessionId: promptAppend.sessionId,
    body: promptAppend.body,
    attachments: (promptAppend.attachments ?? []).map(normalizeAttachment),
    artifacts: promptAppend.artifacts ?? [],
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
    todoList: null,
    artifactCount: 0,
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
