<template>
  <PromptComposer
    :prompt="prompt"
    :files="files"
    :model="model"
    :effort="effort"
    :permission="permission"
    :title="title"
    :placeholder="placeholder"
    :disabled="disabled"
    :compact="compact"
    :show-badge="showBadge"
    :readonly-config="readonlyConfig"
    :model-options="modelOptions"
    @update:prompt="emit('update:prompt', $event)"
    @update:files="emit('update:files', $event)"
    @update:model="emit('update:model', $event)"
    @update:effort="emit('update:effort', $event)"
    @update:permission="emit('update:permission', $event)"
  >
    <template #actions>
      <slot name="actions" />
    </template>
  </PromptComposer>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';

import PromptComposer from '@/components/PromptComposer.vue';
import type { CodexModelOption } from '@/components/promptOptions';
import { listCodexModelOptions } from '@/services/codexOptions';

withDefaults(
  defineProps<{
    prompt: string;
    files: File[];
    model: string;
    effort: string;
    permission: string;
    title?: string;
    placeholder?: string;
    disabled?: boolean;
    compact?: boolean;
    showBadge?: boolean;
    readonlyConfig?: boolean;
  }>(),
  {
    title: '',
    placeholder: '描述你希望 Codex 完成的任务',
    disabled: false,
    compact: false,
    showBadge: true,
    readonlyConfig: false,
  },
);

const emit = defineEmits<{
  'update:prompt': [value: string];
  'update:files': [value: File[]];
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
}>();

const modelOptions = ref<CodexModelOption[]>([]);

onMounted(async () => {
  modelOptions.value = await listCodexModelOptions();
});
</script>
