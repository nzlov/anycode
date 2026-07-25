import { computed, ref, shallowRef } from 'vue';

import { useSessionTranscriptEventLoader } from '@/services/sessionTranscriptContent';
import type { TranscriptItem } from '@/services/sessionTimeline';

export function useDeferredTranscriptEvent(currentEvent: () => TranscriptItem) {
  const loader = useSessionTranscriptEventLoader();
  const loaded = shallowRef<TranscriptItem | null>(null);
  const loading = ref(false);
  const error = ref('');
  const event = computed(() => {
    const current = currentEvent();
    return loaded.value?.id === current.id ? loaded.value : current;
  });

  async function load() {
    const current = currentEvent();
    if (!current.deferred || loaded.value?.id === current.id || loading.value) return;
    if (!loader) {
      error.value = '无法加载事件详情';
      return;
    }
    loading.value = true;
    error.value = '';
    try {
      const result = await loader(current);
      loaded.value = { ...result, id: current.id, sourceEventIds: current.sourceEventIds };
    } catch (err) {
      error.value = err instanceof Error ? err.message : '加载事件详情失败';
    } finally {
      loading.value = false;
    }
  }

  return { event, loading, error, load };
}
