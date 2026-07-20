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
      class="compact-select permission-select"
      hide-dropdown-icon
      :disable="disabled"
      :options="permissionModeOptions"
    >
      <template #prepend>
        <q-icon :name="permissionIcon" />
      </template>
      <q-tooltip>运行权限：{{ permissionLabel }}</q-tooltip>
    </q-select>

    <CodexModelSelector
      :model="model"
      :effort="effort"
      :disabled="disabled"
      :readonly="readonlyConfig"
      @update:model="emit('update:model', $event)"
      @update:effort="emit('update:effort', $event)"
    />

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
import { computed } from 'vue';

import CodexModelSelector from '@/components/CodexModelSelector.vue';
import { permissionModeLabel, permissionModeOptions } from '@/components/promptOptions';

const props = withDefaults(
  defineProps<{
    model: string;
    effort: string;
    permission: string;
    fast: boolean;
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
</script>
