import { ref } from 'vue';

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
  subscribeSessionStateUpdates,
  executeSession as executeSessionRequest,
  submitQuestionBatch,
  submitWorkflowApproval as submitWorkflowApprovalRequest,
  type QuestionAnswerInput,
  type QuestionBatch,
  type PageInfo,
  stopSession as stopSessionRequest,
  updateSessionConfig,
  type SessionConfigInput,
  type SessionDetailData,
} from '@/services/sessions';
import {
  getSessionTranscriptPage,
  subscribeSessionTranscript,
  type TranscriptTokenUsage,
} from '@/services/sessionTimeline';
import {
  appendLiveEvent,
  createLatestRequestTracker,
  mergeSnapshotEvents,
  prependOlderEvents,
  shouldReconnectAfterClose,
} from '@/services/sessionEventTimeline';

const eventPageSize = 50;
const subscriptionReadyTimeoutMs = 3000;
const emptyPageInfo: PageInfo = { page: 1, pageSize: eventPageSize, total: 0, nextCursor: '' };

export function useSessionDetail(sessionId: string) {
  const session = ref<SessionDetailData['session'] | null>(null);
  const events = ref<SessionDetailData['events']>([]);
  const tokenUsage = ref<TranscriptTokenUsage | null>(null);
  const eventsPageInfo = ref<PageInfo>({ ...emptyPageInfo });
  const loading = ref(false);
  const loadingOlderEvents = ref(false);
  const appending = ref(false);
  const executing = ref(false);
  const stopping = ref(false);
  const closing = ref(false);
  const updatingConfig = ref(false);
  const questionsLoading = ref(false);
  const questionsSubmitting = ref(false);
  const approvalSubmitting = ref(false);
  const pendingQuestionBatches = ref<QuestionBatch[]>([]);
  const error = ref('');
  let liveStopped = true;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let eventSubscription: { unsubscribe: () => void } | null = null;
  let stateSubscription: { unsubscribe: () => void } | null = null;
  let bufferingLiveEvents = false;
  let bufferedLiveEvents: SessionDetailData['events'] = [];
  let bufferedLiveUsage: TranscriptTokenUsage | null = null;
  let releaseSubscriptionReadiness: (() => void) | null = null;
  let subscriptionGeneration = 0;
  const eventSnapshotRequests = createLatestRequestTracker();
  const sessionRequests = createLatestRequestTracker();
  const questionRequests = createLatestRequestTracker();
  let accessValidation: Promise<boolean> | null = null;

  async function loadSessionDetail() {
    const eventRequest = eventSnapshotRequests.next();
    const sessionRequest = sessionRequests.next();
    loading.value = true;
    if (!liveStopped && !bufferingLiveEvents) {
      bufferingLiveEvents = true;
      bufferedLiveUsage = null;
    }
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
      if (eventSnapshotRequests.isCurrent(eventRequest)) {
        if (eventResult.status === 'fulfilled') {
          events.value = mergeSnapshotEvents(
            eventResult.value.items,
            events.value,
            bufferedLiveEvents,
          );
          bufferedLiveEvents = [];
          eventsPageInfo.value = eventResult.value.pageInfo;
          tokenUsage.value = bufferedLiveUsage ?? eventResult.value.usage;
        } else {
          error.value =
            eventResult.reason instanceof Error ? eventResult.reason.message : '加载会话事件失败';
        }
      }
    } finally {
      if (eventSnapshotRequests.isCurrent(eventRequest)) {
        events.value = mergeSnapshotEvents([], events.value, bufferedLiveEvents);
        bufferedLiveEvents = [];
        if (bufferedLiveUsage) tokenUsage.value = bufferedLiveUsage;
        bufferedLiveUsage = null;
        bufferingLiveEvents = false;
        loading.value = false;
      }
    }
  }

  async function appendDescription(body: string, stagedAttachmentIds: string[] = []) {
    const text = body.trim();
    if (!text && stagedAttachmentIds.length === 0) return;
    const appendBody = text || '追加附件';

    appending.value = true;
    error.value = '';
    try {
      await appendPrompt(sessionId, appendBody, stagedAttachmentIds);
      await loadSessionDetail();
    } catch (err) {
      const cleanupError = await cleanupStagedAttachments(stagedAttachmentIds);
      const message = err instanceof Error ? err.message : '追加描述失败';
      error.value = cleanupError ? `${message}；${cleanupError}` : message;
      throw err;
    } finally {
      appending.value = false;
    }
  }

  async function stopSession() {
    stopping.value = true;
    error.value = '';
    try {
      await stopSessionRequest(sessionId);
      await loadSessionDetail();
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
      await loadSessionDetail();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '关闭会话失败';
      throw err;
    } finally {
      closing.value = false;
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
      await loadSessionDetail();
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
      await Promise.all([loadSessionDetail(), loadPendingQuestions()]);
    } catch (err) {
      error.value = err instanceof Error ? err.message : '提交回答失败';
      throw err;
    } finally {
      questionsSubmitting.value = false;
    }
  }

  async function submitApproval(approved: boolean, comment: string) {
    const approval = session.value?.pendingApproval;
    if (!approval) {
      error.value = '未找到当前审批上下文，请刷新后重试';
      return;
    }
    approvalSubmitting.value = true;
    error.value = '';
    try {
      await submitWorkflowApprovalRequest({
        workflowRunId: approval.workflowRunId,
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

  async function startLiveUpdates() {
    liveStopped = false;
    bufferingLiveEvents = true;
    bufferedLiveUsage = null;
    await waitForSubscriptionRegistration(openSubscriptions());
  }

  function stopLiveUpdates() {
    liveStopped = true;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    subscriptionGeneration += 1;
    releaseSubscriptionReadiness?.();
    releaseSubscriptionReadiness = null;
    eventSubscription?.unsubscribe();
    stateSubscription?.unsubscribe();
    eventSubscription = null;
    stateSubscription = null;
    bufferingLiveEvents = false;
    bufferedLiveEvents = [];
    bufferedLiveUsage = null;
  }

  function openSubscriptions() {
    const generation = ++subscriptionGeneration;
    releaseSubscriptionReadiness?.();
    eventSubscription?.unsubscribe();
    stateSubscription?.unsubscribe();
    eventSubscription = null;
    stateSubscription = null;
    const transcriptReady = createSubscriptionReady();
    const stateReady = createSubscriptionReady();
    releaseSubscriptionReadiness = () => {
      transcriptReady.release();
      stateReady.release();
    };
    eventSubscription = subscribeSessionTranscript(sessionId, {
      onSubscribed: transcriptReady.resolve,
      onData: (event) => {
        if (generation !== subscriptionGeneration) return;
        if (bufferingLiveEvents) {
          bufferedLiveEvents = appendLiveEvent(bufferedLiveEvents, event);
          return;
        }
        const nextEvents = appendLiveEvent(events.value, event);
        events.value = nextEvents;
      },
      onUsage: (usage) => {
        if (generation !== subscriptionGeneration) return;
        if (bufferingLiveEvents) {
          bufferedLiveUsage = usage;
          return;
        }
        tokenUsage.value = usage;
      },
      onError: (err) => {
        transcriptReady.release();
        if (generation !== subscriptionGeneration) return;
        error.value = err.message;
        if (shouldReconnectLiveError(err)) {
          scheduleReconnect();
        }
      },
      onClose: (close) => {
        transcriptReady.release();
        if (generation === subscriptionGeneration) {
          void handleSubscriptionClose(close, generation);
        }
      },
    });
    stateSubscription = subscribeSessionStateUpdates(sessionId, {
      onSubscribed: stateReady.resolve,
      onData: (update) => {
        if (generation !== subscriptionGeneration) return;
        if (update.session) {
          sessionRequests.invalidate();
          session.value = update.session;
        }
        if (update.questionBatch) {
          questionRequests.invalidate();
          questionsLoading.value = false;
          pendingQuestionBatches.value = mergeQuestionBatch(
            pendingQuestionBatches.value,
            update.questionBatch,
          );
        }
      },
      onError: (err) => {
        stateReady.release();
        if (generation !== subscriptionGeneration) return;
        error.value = err.message;
        if (shouldReconnectLiveError(err)) {
          scheduleReconnect();
        }
      },
      onClose: (close) => {
        stateReady.release();
        if (generation === subscriptionGeneration) {
          void handleSubscriptionClose(close, generation);
        }
      },
    });
    return {
      generation,
      transcriptReady: transcriptReady.promise.then(() => transcriptReady.registered()),
      stateReady: stateReady.promise.then(() => stateReady.registered()),
    };
  }

  function scheduleReconnect() {
    if (liveStopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      void reconnectFromSnapshot();
    }, 1500);
  }

  async function reconnectFromSnapshot() {
    if (liveStopped) return;
    bufferingLiveEvents = true;
    bufferedLiveUsage = null;
    await waitForSubscriptionRegistration(openSubscriptions());
    if (liveStopped) return;
    await Promise.all([loadSessionDetail(), loadPendingQuestions()]);
  }

  async function waitForSubscriptionRegistration(registration: {
    generation: number;
    transcriptReady: Promise<boolean>;
    stateReady: Promise<boolean>;
  }) {
    const [transcriptRegistered, stateRegistered] = await Promise.all([
      waitWithTimeout(registration.transcriptReady, subscriptionReadyTimeoutMs, false),
      waitWithTimeout(registration.stateReady, subscriptionReadyTimeoutMs, false),
    ]);
    if (!transcriptRegistered) {
      void registration.transcriptReady.then((lateRegistered) => {
        if (!lateRegistered || liveStopped || registration.generation !== subscriptionGeneration)
          return;
        void loadSessionDetail();
      });
    }
    if (!stateRegistered) {
      void registration.stateReady.then((lateRegistered) => {
        if (!lateRegistered || liveStopped || registration.generation !== subscriptionGeneration)
          return;
        void Promise.all([loadSessionState(), loadPendingQuestions()]);
      });
    }
  }

  async function handleSubscriptionClose(close: GraphQLSubscriptionClose, generation: number) {
    let accessKeyValid: boolean | undefined;
    if (!close.acknowledged && !close.completedByServer) {
      accessKeyValid = await validateAccessKeyForReconnect();
    }
    if (generation !== subscriptionGeneration || liveStopped) return;
    if (shouldReconnectAfterClose(close.acknowledged, accessKeyValid, close.completedByServer)) {
      scheduleReconnect();
      return;
    }
    if (close.completedByServer) return;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    error.value = '访问密钥无效，请重新登录';
  }

  function validateAccessKeyForReconnect() {
    if (!accessValidation) {
      accessValidation = verifyGraphQLAccessKey(getGraphQLAccessKey())
        .catch(() => true)
        .finally(() => {
          accessValidation = null;
        });
    }
    return accessValidation;
  }

  return {
    session,
    events,
    tokenUsage,
    eventsPageInfo,
    pendingQuestionBatches,
    loading,
    loadingOlderEvents,
    appending,
    executing,
    stopping,
    closing,
    updatingConfig,
    questionsLoading,
    questionsSubmitting,
    approvalSubmitting,
    error,
    loadSessionDetail,
    appendDescription,
    executeSession,
    stopSession,
    closeSession,
    updateConfig,
    loadPendingQuestions,
    loadOlderEvents,
    submitPendingAnswers,
    submitApproval,
    startLiveUpdates,
    stopLiveUpdates,
  };
}

function createSubscriptionReady() {
  let settled = false;
  let subscribed = false;
  let settlePromise: (() => void) | null = null;
  const promise = new Promise<void>((resolve) => {
    settlePromise = resolve;
  });
  const settle = (registered: boolean) => {
    if (settled) return;
    settled = true;
    subscribed = registered;
    settlePromise?.();
    settlePromise = null;
  };
  return {
    promise,
    resolve: () => settle(true),
    release: () => settle(false),
    registered: () => subscribed,
  };
}

function waitWithTimeout<T>(promise: Promise<T>, timeoutMs: number, fallback: T) {
  return new Promise<T>((resolve) => {
    const timer = setTimeout(() => resolve(fallback), timeoutMs);
    void promise.then((value) => {
      clearTimeout(timer);
      resolve(value);
    });
  });
}

function shouldReconnectLiveError(err: Error) {
  return !(err instanceof AnyCodeGraphQLError && err.code === 'auth_failed');
}

function mergeQuestionBatch(existing: QuestionBatch[], batch: QuestionBatch) {
  if (batch.status !== 'pending') {
    return existing.filter((item) => item.id !== batch.id);
  }
  const next = existing.filter((item) => item.id !== batch.id);
  next.push(batch);
  return next;
}

async function cleanupStagedAttachments(ids: string[]) {
  if (ids.length === 0) return '';
  const results = await Promise.allSettled(ids.map((id) => deleteStagedAttachment(id)));
  const failed = results.find((result) => result.status === 'rejected');
  if (!failed || failed.status !== 'rejected') return '';
  const reason = failed.reason instanceof Error ? failed.reason.message : String(failed.reason);
  return `已上传附件清理失败：${reason}`;
}
