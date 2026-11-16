<template>
  <div class="content-view-toggle" role="group" aria-label="内容展示方式" @click.stop @keydown.stop>
    <q-btn
      v-for="view in views"
      :key="view.value"
      flat
      dense
      no-caps
      size="sm"
      :icon="view.icon"
      :label="view.label"
      :aria-pressed="modelValue === view.value"
      :class="{ 'content-view-toggle__active': modelValue === view.value }"
      @click="$emit('update:modelValue', view.value)"
    />
  </div>
</template>
<script setup lang="ts">
defineProps<{ modelValue: 'preview' | 'source' }>();
defineEmits<{ 'update:modelValue': [value: 'preview' | 'source'] }>();
const views = [
  { value: 'preview' as const, icon: 'visibility', label: '预览' },
  { value: 'source' as const, icon: 'code', label: '源码' },
];
</script>
<style scoped>
.content-view-toggle {
  display: inline-flex;
  gap: 2px;
  flex: 0 0 auto;
  padding: 2px;
  border-radius: 6px;
  background: var(--ac-surface-muted);
}
.content-view-toggle :deep(.q-btn) {
  min-height: 28px;
  padding: 2px 8px;
  color: var(--ac-text-muted);
}
.content-view-toggle :deep(.content-view-toggle__active) {
  background: var(--ac-surface);
  color: var(--q-primary);
}
.content-view-toggle :deep(.q-btn:focus-visible) {
  outline: 2px solid var(--q-primary) !important;
  outline-offset: 2px;
}
</style>
