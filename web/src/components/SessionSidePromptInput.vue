<template>
  <div class="side-prompt-input">
    <q-input
      :model-value="modelValue"
      outlined
      autogrow
      autofocus
      type="textarea"
      :label="label"
      :disable="disabled || loading"
      @update:model-value="emit('update:modelValue', String($event ?? ''))"
      @keydown.meta.enter.prevent="submit"
      @keydown.ctrl.enter.prevent="submit"
    />
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
  </div>
</template>

<script setup lang="ts">
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

function submit() {
  if (props.disabled || props.loading || !props.modelValue.trim()) return;
  emit('submit');
}
</script>

<style scoped>
.side-prompt-input {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 10px;
}
</style>
