<template>
  <div
    class="quick-command-editor writable-root-editor"
    :class="{ 'writable-root-editor--inline': inline }"
  >
    <q-input
      ref="inputRef"
      :model-value="modelValue"
      outlined
      dense
      label="绝对路径"
      placeholder="/home/anycode/.cache/go-build"
      :disable="disabled"
      :error="!!error"
      :error-message="error"
      @update:model-value="emit('update:modelValue', String($event ?? ''))"
      @keyup.enter="emit('save')"
    >
      <template #append>
        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="folder_open"
          aria-label="选择白名单目录"
          @click="emit('chooseDirectory')"
        >
          <q-tooltip>选择目录</q-tooltip>
        </q-btn>
      </template>
    </q-input>
    <div class="quick-command-editor__actions">
      <q-btn
        flat
        round
        dense
        class="app-icon-btn"
        icon="close"
        aria-label="取消编辑白名单目录"
        @click="emit('cancel')"
      >
        <q-tooltip>取消</q-tooltip>
      </q-btn>
      <q-btn
        unelevated
        round
        dense
        class="app-icon-btn app-on-primary"
        color="primary"
        icon="save"
        aria-label="保存白名单目录"
        :disable="!valid || disabled"
        @click="emit('save')"
      >
        <q-tooltip>保存</q-tooltip>
      </q-btn>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

defineProps<{
  modelValue: string;
  disabled: boolean;
  error: string;
  valid: boolean;
  inline?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: string];
  chooseDirectory: [];
  save: [];
  cancel: [];
}>();

const inputRef = ref<{ focus: () => void } | null>(null);

defineExpose({
  focus: () => inputRef.value?.focus(),
});
</script>
