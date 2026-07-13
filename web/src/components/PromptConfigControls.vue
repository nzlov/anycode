<template>
  <div class="prompt-config-controls">
    <div v-if="readonlyConfig" class="prompt-config-chip">
      <q-icon :name="permissionIcon" />
      <span>{{ permissionLabel }}</span>
      <q-tooltip>运行权限</q-tooltip>
    </div>
    <q-select
      v-else
      v-model="permissionModel"
      dense
      borderless
      emit-value
      map-options
      aria-label="运行权限"
      class="compact-select"
      :disable="disabled"
      :options="permissionModeOptions"
    >
      <template #prepend>
        <q-icon :name="permissionIcon" />
      </template>
      <q-tooltip>运行权限</q-tooltip>
    </q-select>

    <div v-if="readonlyConfig" class="prompt-config-chip">
      <q-icon name="smart_toy" />
      <span>{{ modelLabel }}</span>
      <q-tooltip>Codex 模型</q-tooltip>
    </div>
    <q-select
      v-else
      v-model="modelModel"
      dense
      borderless
      emit-value
      map-options
      aria-label="Codex 模型"
      class="compact-select"
      :disable="disabled"
      :options="modelOptions"
    >
      <template #prepend>
        <q-icon name="smart_toy" />
      </template>
      <q-tooltip>Codex 模型</q-tooltip>
    </q-select>

    <div v-if="readonlyConfig" class="prompt-config-chip">
      <q-icon name="psychology" />
      <span>{{ effortLabel }}</span>
      <q-tooltip>思考强度</q-tooltip>
    </div>
    <q-select
      v-else
      v-model="effortModel"
      dense
      borderless
      emit-value
      map-options
      aria-label="思考强度"
      class="compact-select"
      :disable="disabled"
      :options="reasoningEffortOptions"
    >
      <template #prepend>
        <q-icon name="psychology" />
      </template>
      <q-tooltip>思考强度</q-tooltip>
    </q-select>

    <q-checkbox
      v-model="fastModel"
      dense
      label="Fast"
      aria-label="Fast 模式"
      class="prompt-fast-checkbox"
      :disable="disabled || readonlyConfig"
    >
      <q-tooltip>使用优先服务层</q-tooltip>
    </q-checkbox>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue';

import {
  type CodexModelOption,
  codexModelLabel,
  normalizeCodexModel,
  normalizeReasoningEffort,
  permissionModeLabel,
  permissionModeOptions,
  promptConfigUpdatesForModelChange,
  reasoningEffortLabel,
  reasoningEffortOptionsForModel,
} from '@/components/promptOptions';

const props = withDefaults(
  defineProps<{
    model: string;
    effort: string;
    permission: string;
    fast: boolean;
    modelOptions: CodexModelOption[];
    disabled?: boolean;
    readonlyConfig?: boolean;
  }>(),
  {
    disabled: false,
    readonlyConfig: false,
  },
);

const emit = defineEmits<{
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
  'update:fast': [value: boolean];
}>();

const modelModel = computed({
  get: () => props.model,
  set: (value: string) => {
    for (const update of promptConfigUpdatesForModelChange(
      props.modelOptions,
      value,
      props.effort,
    )) {
      if (update.field === 'model') {
        emit('update:model', update.value);
      } else {
        emit('update:effort', update.value);
      }
    }
  },
});
const effortModel = computed({
  get: () => props.effort,
  set: (value: string) =>
    emit('update:effort', normalizeReasoningEffort(props.modelOptions, props.model, value)),
});
const permissionModel = computed({
  get: () => props.permission,
  set: (value: string) => emit('update:permission', value),
});
const fastModel = computed({
  get: () => props.fast,
  set: (value: boolean) => emit('update:fast', value),
});
const permissionIcon = computed(
  () =>
    permissionModeOptions.find((option) => option.value === props.permission)?.icon ?? 'edit_note',
);
const permissionLabel = computed(() => permissionModeLabel(props.permission));
const modelLabel = computed(() => codexModelLabel(props.modelOptions, props.model));
const effortLabel = computed(() =>
  reasoningEffortLabel(props.modelOptions, props.model, props.effort),
);
const reasoningEffortOptions = computed(() =>
  reasoningEffortOptionsForModel(props.modelOptions, props.model),
);

watch(
  () => [props.model, props.modelOptions] as const,
  ([value]) => {
    const nextModel = normalizeCodexModel(props.modelOptions, value);
    if (nextModel !== value) {
      emit('update:model', nextModel);
      return;
    }
    const nextEffort = normalizeReasoningEffort(props.modelOptions, nextModel, props.effort);
    if (nextEffort !== props.effort) {
      emit('update:effort', nextEffort);
    }
  },
  { immediate: true },
);

watch(
  () => [props.effort, props.modelOptions] as const,
  ([value]) => {
    const nextEffort = normalizeReasoningEffort(props.modelOptions, props.model, value);
    if (nextEffort !== value) {
      emit('update:effort', nextEffort);
    }
  },
  { immediate: true },
);
</script>
