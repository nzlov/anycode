<template>
  <q-page class="surface-page">
    <PromptAppendEditPanel
      v-model:body="body"
      :target="target"
      :saving="saving"
      :error="error"
      :can-save="canSave"
      @cancel="close"
      @save="save"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import PromptAppendEditPanel from '@/components/PromptAppendEditPanel.vue';
import { getSession, updatePromptAppend, type PromptAppend } from '@/services/sessions';

const route = useRoute();
const router = useRouter();
const sessionId = computed(() => String(route.params.id ?? ''));
const promptId = computed(() => String(route.params.promptId ?? ''));
const target = ref<PromptAppend | null>(null);
const body = ref('');
const saving = ref(false);
const error = ref('');
const canSave = computed(() => {
  const text = body.value.trim();
  return Boolean(target.value && text && text !== target.value.body.trim() && !saving.value);
});

onMounted(async () => {
  try {
    const session = await getSession(sessionId.value);
    target.value = session.promptAppends.find((item) => item.id === promptId.value) ?? null;
    if (!target.value) {
      error.value = '追加提示已不存在';
      return;
    }
    body.value = target.value.body;
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载追加提示失败';
  }
});

async function save() {
  if (!target.value || !canSave.value) return;
  saving.value = true;
  error.value = '';
  try {
    await updatePromptAppend(sessionId.value, target.value.id, body.value.trim());
    close();
  } catch (err) {
    error.value = err instanceof Error ? err.message : '保存追加提示失败';
  } finally {
    saving.value = false;
  }
}

function close() {
  void router.push({ name: 'session-detail', params: { id: sessionId.value } });
}
</script>
