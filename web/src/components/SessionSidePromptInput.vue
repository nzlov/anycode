<template>
  <CodexPromptComposer
    v-model:prompt="message.prompt"
    v-model:files="message.files"
    v-model:model="message.config.codexModel"
    v-model:effort="message.config.reasoningEffort"
    v-model:fast="message.config.fastMode"
    class="side-prompt-input"
    permission="read-only"
    readonly-permission
    force-config-menu
    :placeholder="label"
    :disabled="disabled || loading"
    compact
    collapsible
    :collapsed="collapsed"
    :show-badge="false"
    @update:collapsed="collapsed = $event"
    @submit="submit"
  >
    <template #actions>
      <q-btn
        unelevated
        round
        color="primary"
        icon="send"
        aria-label="发送 Side 提问"
        :loading="loading"
        :disable="disabled || !canSubmit"
        @click="submit"
      >
        <q-tooltip>发送 Side 提问</q-tooltip>
      </q-btn>
    </template>
  </CodexPromptComposer>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import CodexPromptComposer from '@/components/CodexPromptComposer.vue';
import type { SessionSideMessage } from '@/services/sessionSides';

const message = defineModel<SessionSideMessage>({ required: true });

const props = withDefaults(
  defineProps<{
    label?: string;
    loading?: boolean;
    disabled?: boolean;
  }>(),
  { label: '输入临时问题', loading: false, disabled: false },
);

const emit = defineEmits<{
  submit: [];
}>();
const collapsed = ref(false);
const canSubmit = computed(() =>
  Boolean(message.value.prompt.trim() || message.value.files.length),
);

function submit() {
  if (props.disabled || props.loading || !canSubmit.value) return;
  emit('submit');
}
</script>

<style scoped>
.side-prompt-input :deep(.prompt-input .q-field__native) {
  max-height: min(220px, 30dvh);
  overflow-y: auto !important;
}
</style>
