import { ref } from 'vue';

import {
  appendPrompt,
  getSessionDetail,
  stopSession as stopSessionRequest,
  type SessionDetailData,
} from '@/services/sessions';

export function useSessionDetail(sessionId: string) {
  const session = ref<SessionDetailData['session'] | null>(null);
  const events = ref<SessionDetailData['events']>([]);
  const loading = ref(false);
  const appending = ref(false);
  const stopping = ref(false);
  const error = ref('');

  async function loadSessionDetail() {
    loading.value = true;
    error.value = '';
    try {
      const result = await getSessionDetail(sessionId);
      session.value = result.session;
      events.value = result.events;
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载会话详情失败';
    } finally {
      loading.value = false;
    }
  }

  async function appendDescription(body: string) {
    const text = body.trim();
    if (!text) return;

    appending.value = true;
    error.value = '';
    try {
      await appendPrompt(sessionId, text);
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

  return {
    session,
    events,
    loading,
    appending,
    stopping,
    error,
    loadSessionDetail,
    appendDescription,
    stopSession,
  };
}
