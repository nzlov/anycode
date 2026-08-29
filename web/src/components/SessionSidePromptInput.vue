<template>
  <PromptComposer
    class="side-prompt-input"
    :prompt="modelValue"
    :placeholder="label"
    :disabled="disabled || loading"
    compact
    collapsible
    :collapsed="collapsed"
    :show-badge="false"
    :show-config="false"
    :allow-attachments="false"
    @update:prompt="emit('update:modelValue', $event)"
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
        :disable="disabled || !modelValue.trim()"
        @click="submit"
      >
        <q-tooltip>发送 Side 提问</q-tooltip>
      </q-btn>
    </template>
  </PromptComposer>
</template>

<script setup lang="ts">
import { ref } from 'vue';

import PromptComposer from '@/components/PromptComposer.vue';

const props = withDefaults(
  defineProps<{
    modelValue: string;
    label?: string;
    loading?: boolean;
    disabled?: boolean;
  }>(),
  { label: '输入临时问题', loading: false, disabled: false },
);

const emit = defineEmits<{
  'update:modelValue': [value: string];
  submit: [];
}>();
const collapsed = ref(false);

function submit() {
  if (props.disabled || props.loading || !props.modelValue.trim()) return;
  emit('submit');
}
</script>

<style scoped>
.side-prompt-input :deep(.prompt-input .q-field__native) {
  max-height: min(220px, 30dvh);
  overflow-y: auto !important;
}
</style>
