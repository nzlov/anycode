import { ref } from 'vue';

import { useSessionUpdates } from '@/composables/useSessionUpdates';
import { deleteStagedAttachment } from '@/services/attachments';
import {
  AnyCodeGraphQLError,
  getGraphQLAccessKey,
  type GraphQLSubscriptionClose,
  verifyGraphQLAccessKey,
} from '@/services/graphqlClient';
import { olderTranscriptCursor } from '@/services/sessionEventPaging';
import {
  appendPrompt,
  closeSession as closeSessionRequest,
  getPendingQuestionBatches,
  getSession,
  subscribeSessionEvents,
  executeSession as executeSessionRequest,
  retrySessionWorktreeCleanup as retrySessionWorktreeCleanupRequest,
  submitQuestionBatch,
  submitWorkflowApproval as submitWorkflowApprovalRequest,
  type QuestionAnswerInput,
  type QuestionBatch,
  type PageInfo,
  stopSession as stopSessionRequest,
  updateSessionConfig,
  updatePromptAppend,
  type SessionConfigInput,
  type SessionDetailData,
  type SessionUpdateEvent,
} from '@/services/sessions';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';
import {
  getSessionTranscriptPage,
  type TranscriptTokenUsage,
  type TranscriptUsageAttribution,
} from '@/services/sessionTimeline';
import {
  appendLiveEvent,
  createLatestRequestTracker,
  prependOlderEvents,
  shouldReconnectSubscription,
} from '@/services/sessionEventTimeline';

const eventPageSize = 50;
const emptyPageInfo: PageInfo = { page: 1, pageSize: eventPageSize, total: 0, nextCursor: '' };

export function useSessionDetail(sessionId: string) {
  const session = ref<SessionDetailData['session'] | null>(null);
  const events = ref<SessionDetailData['events']>([]);
  const tokenUsage = ref<TranscriptTokenUsage | null>(null);
  const nodeUsage = ref<TranscriptUsageAttribution[]>([]);
  const eventsPageInfo = ref<PageInfo>({ ...emptyPageInfo });
  const loading = ref(false);
  const loadingOlderEvents = ref(false);
  const appending = ref(false);
  const executing = ref(false);
  const stopping = ref(false);
  const closing = ref(false);
  const retryingWorktreeCleanup = ref(false);
  const updatingConfig = ref(false);
  const questionsLoading = ref(false);
  const questionsSubmitting = ref(false);
  const approvalLoading = ref(false);
  const approvalSubmitting = ref(false);
  const pendingQuestionBatches = ref<QuestionBatch[]>([]);
  const artifactUpdateVersion = ref(0);
  const diffUpdateVersion = ref(0);
  const error = ref('');
  let liveStopped = true;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let sessionEventSubscription: { unsubscribe: () => void } | null = null;
  let subscriptionGeneration = 0;
  let approvalLoadGeneration = 0;
  const detailRequests = createLatestRequestTracker();
  const sessionRequests = createLatestRequestTracker();
  const questionRequests = createLatestRequestTracker();

  const { start: startSessionUpdates, stop: stopSessionUpdates } = useSessionUpdates({
    onData: handleSessionUpdate,
    onError: (updateError) => {
      error.value = updateError.message;
    },
  });

  async function loadSessionDetail() {
    const detailRequest = detailRequests.next();
    const sessionRequest = sessionRequests.next();
    loading.value = true;
    error.value = '';
    try {
      const [sessionResult, eventResult] = await Promise.allSettled([
        getSession(sessionId),
        getSessionTranscriptPage(sessionId, '', eventPageSize),
      ]);
      if (sessionRequests.isCurrent(sessionRequest)) {
        if (sessionResult.status === 'fulfilled') {
          session.value = sessionResult.value;
        } else {
          error.value =
            sessionResult.reason instanceof Error
              ? sessionResult.reason.message
              : '加载会话状态失败';
        }
      }
      if (detailRequests.isCurrent(detailRequest)) {
        if (eventResult.status === 'fulfilled') {
          events.value = eventResult.value.items;
          eventsPageInfo.value = eventResult.value.pageInfo;
          tokenUsage.value = eventResult.value.usage;
          nodeUsage.value = eventResult.value.nodeUsage;
        } else {
          error.value =
            eventResult.reason instanceof Error ? eventResult.reason.message : '加载会话事件失败';
        }
      }
    } finally {
      if (detailRequests.isCurrent(detailRequest)) {
        loading.value = false;
      }
    }
  }

  async function appendDescription(
    body: string,
    stagedAttachmentIds: string[] = [],
    artifactIds: string[] = [],
  ) {
    const text = body.trim();
    if (!text && stagedAttachmentIds.length === 0 && artifactIds.length === 0) return;

    appending.value = true;
    error.value = '';
    try {
      await appendPrompt(sessionId, text, stagedAttachmentIds, artifactIds);
      await loadSessionState();
    } catch (err) {
      const cleanupError = await cleanupStagedAttachments(stagedAttachmentIds);
      const message = err instanceof Error ? err.message : '追加描述失败';
      error.value = cleanupError ? `${message}；${cleanupError}` : message;
      throw err;
    } finally {
      appending.value = false;
    }
  }

  async function updatePromptAppendBody(promptAppendId: string, body: string) {
    const updated = await updatePromptAppend(sessionId, promptAppendId, body.trim());
    const current = session.value;
    if (current) {
      sessionRequests.invalidate();
      session.value = {
        ...current,
        promptAppends: current.promptAppends.map((prompt) =>
          prompt.id === updated.id ? updated : prompt,
        ),
      };
    }
    return updated;
  }

  async function stopSession() {
    stopping.value = true;
    error.value = '';
    try {
      await stopSessionRequest(sessionId);
      await loadSessionState();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '停止会话失败';
    } finally {
      stopping.value = false;
    }
  }

  async function closeSession() {
    closing.value = true;
    error.value = '';
    try {
      await closeSessionRequest(sessionId);
      await loadSessionState();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '关闭会话失败';
      throw err;
    } finally {
      closing.value = false;
    }
  }

  async function retryWorktreeCleanup() {
    retryingWorktreeCleanup.value = true;
    error.value = '';
    try {
      await retrySessionWorktreeCleanupRequest(sessionId);
      await loadSessionState();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '重试工作树清理失败';
      throw err;
    } finally {
      retryingWorktreeCleanup.value = false;
    }
  }

  async function updateConfig(config: SessionConfigInput) {
    updatingConfig.value = true;
    error.value = '';
    try {
      const next = await updateSessionConfig(sessionId, config);
      if (session.value) {
        sessionRequests.invalidate();
        session.value = { ...session.value, config: next.config, updatedAt: next.updatedAt };
      }
    } catch (err) {
      error.value = err instanceof Error ? err.message : '更新会话配置失败';
      throw err;
    } finally {
      updatingConfig.value = false;
    }
  }

  async function executeSession() {
    executing.value = true;
    error.value = '';
    try {
      await executeSessionRequest(sessionId, session.value?.status === 'queued');
      await loadSessionState();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '运行会话失败';
      throw err;
    } finally {
      executing.value = false;
    }
  }

  async function loadPendingQuestions() {
    const requestGeneration = questionRequests.next();
    questionsLoading.value = true;
    error.value = '';
    try {
      const batches = await getPendingQuestionBatches(sessionId);
      if (questionRequests.isCurrent(requestGeneration)) {
        pendingQuestionBatches.value = batches;
      }
    } catch (err) {
      if (questionRequests.isCurrent(requestGeneration)) {
        error.value = err instanceof Error ? err.message : '加载待回答问题失败';
      }
    } finally {
      if (questionRequests.isCurrent(requestGeneration)) {
        questionsLoading.value = false;
      }
    }
  }

  async function loadSessionState() {
    const requestGeneration = sessionRequests.next();
    try {
      const next = await getSession(sessionId);
      if (sessionRequests.isCurrent(requestGeneration)) {
        session.value = next;
      }
    } catch (err) {
      if (sessionRequests.isCurrent(requestGeneration)) {
        error.value = err instanceof Error ? err.message : '加载会话状态失败';
      }
    }
  }

  async function loadOlderEvents(): Promise<string | null> {
    const beforeEventId = olderTranscriptCursor(eventsPageInfo.value);
    if (loadingOlderEvents.value || beforeEventId === null) return null;
    loadingOlderEvents.value = true;
    error.value = '';
    try {
      const result = await getSessionTranscriptPage(sessionId, beforeEventId, eventPageSize);
      events.value = prependOlderEvents(events.value, result.items);
      eventsPageInfo.value = result.pageInfo;
      return result.pageInfo.nextCursor || null;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载历史事件失败';
      throw err;
    } finally {
      loadingOlderEvents.value = false;
    }
  }

  async function submitPendingAnswers(batchId: string, answers: QuestionAnswerInput[]) {
    questionsSubmitting.value = true;
    error.value = '';
    try {
      await submitQuestionBatch(batchId, answers);
      await Promise.all([loadSessionState(), loadPendingQuestions()]);
    } catch (err) {
      error.value = err instanceof Error ? err.message : '提交回答失败';
      throw err;
    } finally {
      questionsSubmitting.value = false;
    }
  }

  async function submitApproval(approved: boolean, comment: string) {
    if (approvalSubmitting.value) return;
    const approval = session.value?.pendingApproval;
    if (!isPendingApprovalReviewable(approval)) {
      error.value = approval
        ? '执行后审批缺少节点结果，请刷新后重试'
        : '未找到当前审批上下文，请刷新后重试';
      return;
    }
    approvalSubmitting.value = true;
    error.value = '';
    try {
      await submitWorkflowApprovalRequest({
        sessionId: approval.sessionId,
        nodeId: approval.nodeId,
        approved,
        comment,
      });
      await loadSessionState();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '提交审批失败';
      throw err;
    } finally {
      approvalSubmitting.value = false;
    }
  }

  function startLiveUpdates() {
    if (!liveStopped) return;
    liveStopped = false;
    openSessionEvents();
    startSessionUpdates();
  }

  function stopLiveUpdates() {
    liveStopped = true;
    approvalLoadGeneration += 1;
    subscriptionGeneration += 1;
    sessionEventSubscription?.unsubscribe();
    sessionEventSubscription = null;
    stopSessionUpdates();
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function openSessionEvents() {
    if (liveStopped) return;
    const generation = ++subscriptionGeneration;
    sessionEventSubscription?.unsubscribe();
    sessionEventSubscription = subscribeSessionEvents(sessionId, {
      onData: (event) => {
        if (generation === subscriptionGeneration) {
          events.value = appendLiveEvent(events.value, event);
        }
      },
      onError: (subscriptionError) => {
        if (generation !== subscriptionGeneration) return;
        error.value = subscriptionError.message;
        if (shouldReconnectLiveError(subscriptionError)) scheduleReconnect();
      },
      onClose: (close) => {
        if (generation === subscriptionGeneration) {
          void handleSubscriptionClose(close, generation);
        }
      },
    });
  }

  function scheduleReconnect() {
    if (liveStopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      openSessionEvents();
    }, 1500);
  }

  async function handleSubscriptionClose(close: GraphQLSubscriptionClose, generation: number) {
    const reconnect = await shouldReconnectSubscription(close, () =>
      verifyGraphQLAccessKey(getGraphQLAccessKey()),
    );
    if (generation !== subscriptionGeneration || liveStopped) return;
    if (reconnect) {
      scheduleReconnect();
    } else if (!close.completedByServer) {
      error.value = '访问密钥无效，请重新登录';
    }
  }

  function handleSessionUpdate(update: SessionUpdateEvent) {
    if (update.sessionId !== sessionId) return;
    const current = session.value;
    const previousStatus = current?.status;
    let next = current;

    if (current && update.status) {
      next = {
        ...current,
        status: update.status.status,
        node: update.status.node,
        availableActions: update.status.availableActions,
        updatedAt: update.status.updatedAt,
        updatedTime: update.status.updatedTime,
      };
    }
    if (next && update.eventType === 'session.todo_list_updated') {
      next = { ...next, todoList: update.todoList ?? null };
    }
    if (next && typeof update.filesChanged === 'number') {
      next = { ...next, filesChanged: update.filesChanged };
      diffUpdateVersion.value += 1;
    }
    if (next && typeof update.artifactCount === 'number') {
      next = { ...next, artifactCount: update.artifactCount };
      artifactUpdateVersion.value += 1;
    }
    if (next && update.priority) {
      next = { ...next, priority: update.priority };
    }
    if (next && update.config) {
      next = { ...next, config: update.config };
    }
    if (next && update.worktreeCleanup) {
      next = { ...next, worktreeCleanup: update.worktreeCleanup };
    }
    if (next && update.availableActions !== undefined) {
      next = { ...next, availableActions: update.availableActions };
    }
    if (next && update.updatedAt && update.updatedTime) {
      next = { ...next, updatedAt: update.updatedAt, updatedTime: update.updatedTime };
    }
    if (update.usage) tokenUsage.value = update.usage;
    if (next !== current) {
      sessionRequests.invalidate();
      session.value = next;
    }

    const status = update.status?.status;
    if (status === 'waiting_user') {
      if (session.value?.pendingApproval) {
        session.value = { ...session.value, pendingApproval: null };
      }
      if (previousStatus !== 'waiting_user') void loadPendingQuestions();
    } else if (status) {
      questionRequests.invalidate();
      questionsLoading.value = false;
      pendingQuestionBatches.value = [];
    }

    if (status === 'waiting_approval') {
      const generation = ++approvalLoadGeneration;
      approvalLoading.value = true;
      void loadSessionState().finally(() => {
        if (generation === approvalLoadGeneration) approvalLoading.value = false;
      });
    } else if (status) {
      approvalLoadGeneration += 1;
      approvalLoading.value = false;
      if (session.value?.pendingApproval) {
        session.value = { ...session.value, pendingApproval: null };
      }
    }
  }

  return {
    session,
    events,
    tokenUsage,
    nodeUsage,
    eventsPageInfo,
    pendingQuestionBatches,
    artifactUpdateVersion,
    diffUpdateVersion,
    loading,
    loadingOlderEvents,
    appending,
    executing,
    stopping,
    closing,
    retryingWorktreeCleanup,
    updatingConfig,
    questionsLoading,
    questionsSubmitting,
    approvalLoading,
    approvalSubmitting,
    error,
    loadSessionDetail,
    appendDescription,
    updatePromptAppendBody,
    executeSession,
    stopSession,
    closeSession,
    retryWorktreeCleanup,
    updateConfig,
    loadPendingQuestions,
    loadOlderEvents,
    submitPendingAnswers,
    submitApproval,
    startLiveUpdates,
    stopLiveUpdates,
  };
}

function shouldReconnectLiveError(err: Error) {
  return !(err instanceof AnyCodeGraphQLError && err.code === 'auth_failed');
}

async function cleanupStagedAttachments(ids: string[]) {
  if (ids.length === 0) return '';
  const results = await Promise.allSettled(ids.map((id) => deleteStagedAttachment(id)));
  const failed = results.find((result) => result.status === 'rejected');
  if (!failed || failed.status !== 'rejected') return '';
  const reason = failed.reason instanceof Error ? failed.reason.message : String(failed.reason);
  return `已上传附件清理失败：${reason}`;
}
