import { ref } from 'vue';

import { AnyCodeGraphQLError } from '@/services/graphqlClient';
import {
  appendPrompt,
  closeSession as closeSessionRequest,
  getPendingQuestionBatches,
  getSessionEventPage,
  getSessionDetail,
  subscribePendingQuestionBatches,
  subscribeSessionEvents,
  resumeSession as resumeSessionRequest,
  startSession as startSessionRequest,
  submitQuestionBatch,
  type QuestionAnswerInput,
  type QuestionBatch,
  type PageInfo,
  stopSession as stopSessionRequest,
  type SessionDetailData,
} from '@/services/sessions';

const eventPageSize = 50;
const emptyPageInfo: PageInfo = { page: 1, pageSize: eventPageSize, total: 0, nextCursor: '' };

export function useSessionDetail(sessionId: string) {
  const session = ref<SessionDetailData['session'] | null>(null);
  const events = ref<SessionDetailData['events']>([]);
  const eventsPageInfo = ref<PageInfo>({ ...emptyPageInfo });
  const loading = ref(false);
  const loadingOlderEvents = ref(false);
  const appending = ref(false);
  const starting = ref(false);
  const resuming = ref(false);
  const stopping = ref(false);
  const closing = ref(false);
  const questionsLoading = ref(false);
  const questionsSubmitting = ref(false);
  const pendingQuestionBatches = ref<QuestionBatch[]>([]);
  const error = ref('');
  let liveStopped = true;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let eventSubscription: { unsubscribe: () => void } | null = null;
  let questionSubscription: { unsubscribe: () => void } | null = null;

  async function loadSessionDetail() {
    loading.value = true;
    error.value = '';
    try {
      const result = await getSessionDetail(sessionId);
      session.value = result.session;
      events.value = result.events;
      eventsPageInfo.value = result.eventsPageInfo;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载会话详情失败';
    } finally {
      loading.value = false;
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
      error.value = err instanceof Error ? err.message : '追加描述失败';
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

  async function startSession() {
    starting.value = true;
    error.value = '';
    try {
      await startSessionRequest(sessionId, session.value?.status === 'queued');
      await loadSessionDetail();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '运行会话失败';
      throw err;
    } finally {
      starting.value = false;
    }
  }

  async function resumeSession() {
    resuming.value = true;
    error.value = '';
    try {
      await resumeSessionRequest(sessionId, session.value?.status === 'queued');
      await loadSessionDetail();
    } catch (err) {
      error.value = err instanceof Error ? err.message : '恢复会话失败';
      throw err;
    } finally {
      resuming.value = false;
    }
  }

  async function loadPendingQuestions() {
    questionsLoading.value = true;
    error.value = '';
    try {
      pendingQuestionBatches.value = await getPendingQuestionBatches(sessionId);
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载待回答问题失败';
    } finally {
      questionsLoading.value = false;
    }
  }

  async function loadOlderEvents() {
    if (loadingOlderEvents.value || eventsPageInfo.value.page <= 1) return;
    loadingOlderEvents.value = true;
    error.value = '';
    try {
      const previousPage = eventsPageInfo.value.page - 1;
      const result = await getSessionEventPage(sessionId, previousPage, eventPageSize);
      const existing = new Set(events.value.map((event) => event.id));
      events.value = [...result.items.filter((event) => !existing.has(event.id)), ...events.value];
      eventsPageInfo.value = result.pageInfo;
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

  function startLiveUpdates() {
    liveStopped = false;
    openSubscriptions();
  }

  function stopLiveUpdates() {
    liveStopped = true;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    eventSubscription?.unsubscribe();
    questionSubscription?.unsubscribe();
    eventSubscription = null;
    questionSubscription = null;
  }

  function openSubscriptions() {
    eventSubscription?.unsubscribe();
    questionSubscription?.unsubscribe();
    const afterEventId = events.value.at(-1)?.id;
    eventSubscription = subscribeSessionEvents(
      { sessionId, ...(afterEventId ? { afterEventId } : {}) },
      {
        onData: (event) => {
          if (!events.value.some((item) => item.id === event.id)) {
            events.value = [...events.value, event];
          }
          if (event.rawType.startsWith('session.') || event.rawType.startsWith('workflow.')) {
            void loadSessionDetail();
          }
        },
        onError: (err) => {
          error.value = err.message;
          if (shouldReconnectLiveError(err)) {
            scheduleReconnect();
          }
        },
        onClose: scheduleReconnect,
      },
    );
    questionSubscription = subscribePendingQuestionBatches(sessionId, {
      onData: (batch) => {
        pendingQuestionBatches.value = mergeQuestionBatch(pendingQuestionBatches.value, batch);
        if (batch.status !== 'pending') {
          void loadSessionDetail();
        }
      },
      onError: (err) => {
        error.value = err.message;
        if (shouldReconnectLiveError(err)) {
          scheduleReconnect();
        }
      },
      onClose: scheduleReconnect,
    });
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
    await Promise.all([loadSessionDetail(), loadPendingQuestions()]);
    if (!liveStopped) {
      openSubscriptions();
    }
  }

  return {
    session,
    events,
    eventsPageInfo,
    pendingQuestionBatches,
    loading,
    loadingOlderEvents,
    appending,
    starting,
    resuming,
    stopping,
    closing,
    questionsLoading,
    questionsSubmitting,
    error,
    loadSessionDetail,
    appendDescription,
    startSession,
    resumeSession,
    stopSession,
    closeSession,
    loadPendingQuestions,
    loadOlderEvents,
    submitPendingAnswers,
    startLiveUpdates,
    stopLiveUpdates,
  };
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
