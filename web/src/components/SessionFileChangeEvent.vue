<template>
  <article class="session-file-change">
    <button
      type="button"
      class="session-file-change__header"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="edit_note" size="16px" />
      <span>{{ event.title }}</span>
      <time>{{ event.time }}</time>
    </button>
    <div v-if="expanded" class="session-file-change__body">
      <div v-if="event.fileChanges?.length" class="session-file-change__list">
        <div v-for="change in event.fileChanges" :key="`${change.kind}:${change.path}`" class="session-file-change__item">
          <span class="session-file-change__kind">{{ fileChangeKindText(change.kind) }}</span>
          <code>{{ change.path }}</code>
        </div>
      </div>
      <pre v-else>{{ event.body || '已记录文件修改' }}</pre>
    </div>
  </article>
</template>

<script setup lang="ts">
import { ref } from 'vue';

import type { SessionEventMessageEntry } from '@/components/SessionEventMessage.vue';

defineProps<{
  event: SessionEventMessageEntry;
}>();

const expanded = ref(false);

function fileChangeKindText(kind: string) {
  const labels: Record<string, string> = {
    add: '新增',
    create: '新增',
    delete: '删除',
    remove: '删除',
    update: '修改',
    modify: '修改',
    rename: '重命名',
  };
  return labels[kind] ?? kind;
}
</script>

<style scoped>
.session-file-change {
  min-width: 0;
}

.session-file-change__header {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: color-mix(in srgb, var(--q-warning) 9%, var(--ac-surface-muted));
  color: var(--ac-text);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  text-align: left;
}

.session-file-change__header span {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.session-file-change__header time {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 400;
}

.session-file-change__header:hover,
.session-file-change__header:focus-visible {
  border-color: color-mix(in srgb, var(--q-warning) 55%, var(--ac-border));
  outline: none;
}

.session-file-change__body {
  margin-top: 6px;
  padding: 8px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
}

.session-file-change__list {
  display: grid;
  gap: 6px;
}

.session-file-change__item {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: 8px;
  align-items: start;
  color: var(--ac-text);
  font-size: 13px;
  line-height: 1.5;
}

.session-file-change__kind {
  color: var(--ac-text-muted);
}

.session-file-change__item code,
.session-file-change__body pre {
  margin: 0;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  word-break: break-word;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12px;
}
</style>
