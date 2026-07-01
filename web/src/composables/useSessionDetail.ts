import { ref } from 'vue';

import { getSessionById, sessionEvents as mockSessionEvents } from '@/mocks/workbench';
import { appendPrompt, getSessionDetail, type SessionDetailData } from '@/services/sessions';

export function useSessionDetail(sessionId: string) {
  const session = ref<SessionDetailData['session']>(getSessionById(sessionId));
  const events = ref<SessionDetailData['events']>([...mockSessionEvents]);
  const loading = ref(false);
  const appending = ref(false);

  async function loadSessionDetail() {
    loading.value = true;
    try {
      const result = await getSessionDetail(sessionId);
      session.value = result.session;
      events.value = result.events;
    } finally {
      loading.value = false;
    }
  }

  async function appendDescription(body: string) {
    const text = body.trim();
    if (!text) return;

    appending.value = true;
    try {
      const result = await appendPrompt(sessionId, text);
      events.value.push({
        id: result.appendPrompt.id,
        kind: 'assistant',
        title: '追加描述',
        body: result.appendPrompt.body,
        time: new Intl.DateTimeFormat('zh-CN', {
          hour: '2-digit',
          minute: '2-digit',
        }).format(new Date(result.appendPrompt.createdAt)),
      });
    } finally {
      appending.value = false;
    }
  }

  return {
    session,
    events,
    loading,
    appending,
    loadSessionDetail,
    appendDescription,
  };
}
