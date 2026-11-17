<template>
  <div class="content-view-toggle" role="group" aria-label="内容展示方式" @click.stop @keydown.stop>
    <q-btn
      flat
      round
      dense
      size="sm"
      :icon="modelValue === 'source' ? 'preview' : 'code'"
      :aria-label="modelValue === 'source' ? '显示预览' : '显示源码'"
      :aria-pressed="modelValue === 'source'"
      @click="$emit('update:modelValue', modelValue === 'source' ? 'preview' : 'source')"
    >
      <q-tooltip>{{ modelValue === 'source' ? '显示预览' : '显示源码' }}</q-tooltip>
    </q-btn>
  </div>
</template>
<script setup lang="ts">
defineProps<{ modelValue: 'preview' | 'source' }>();
defineEmits<{ 'update:modelValue': [value: 'preview' | 'source'] }>();
</script>
<style scoped>
.content-view-toggle {
  display: inline-flex;
  flex: 0 0 auto;
}
.content-view-toggle :deep(.q-btn) {
  color: var(--ac-text-muted);
}
.content-view-toggle :deep(.q-btn:focus-visible) {
  outline: 2px solid var(--q-primary) !important;
  outline-offset: 2px;
}
</style>
