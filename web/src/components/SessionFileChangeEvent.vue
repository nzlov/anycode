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
      <span>{{ fileChangeLabel(content.changes) }}</span>
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <div v-if="expanded" class="session-file-change__body">
      <DiffViewer v-if="diffFileChanges.length" :file-diffs="diffFileChanges" />
      <div v-if="plainFileChanges.length" class="session-file-change__list">
        <div
          v-for="change in plainFileChanges"
          :key="`${change.kind}:${change.path}`"
          class="session-file-change__item"
        >
          <div class="session-file-change__meta">
            <span class="session-file-change__kind">{{ fileChangeKindLabel(change.kind) }}</span>
            <code>{{ change.path }}</code>
          </div>
          <div v-if="change.movePath" class="session-file-change__move">
            <span>目标</span>
            <code>{{ change.movePath }}</code>
          </div>
        </div>
      </div>
      <pre v-if="diffFileChanges.length === 0 && plainFileChanges.length === 0">已记录文件修改</pre>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import DiffViewer from '@/components/DiffViewer.vue';
import { fileDiffFromUnifiedDiff } from '@/services/sessionFileChangeDiff';
import type {
  TranscriptFileChangeContent,
  TranscriptFileChange,
  TranscriptItem,
} from '@/services/sessionTimeline';
import {
  fileChangeKindLabel,
  fileChangeLabel,
  timelineTime,
} from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptFileChangeContent };
}>();

const expanded = ref(false);
const content = computed(() => props.event.content);
const fileChangePresentations = computed(() =>
  content.value.changes.map((change) => ({
    change,
    diff: change.unifiedDiff
      ? fileDiffFromUnifiedDiff(change.path, diffStatus(change.kind), change.unifiedDiff)
      : null,
  })),
);
const diffFileChanges = computed(() =>
  fileChangePresentations.value.flatMap(({ diff }) => (diff ? [diff] : [])),
);
const plainFileChanges = computed(() =>
  fileChangePresentations.value.flatMap(({ change, diff }) => (diff ? [] : [change])),
);

function diffStatus(kind: TranscriptFileChange['kind']) {
  if (kind === 'added') return 'added';
  if (kind === 'deleted') return 'deleted';
  if (kind === 'renamed') return 'renamed';
  return 'modified';
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
  gap: 8px;
}

.session-file-change__item {
  display: grid;
  gap: 6px;
  min-width: 0;
}

.session-file-change__meta,
.session-file-change__move {
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

.session-file-change__move span {
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

.session-file-change__body :deep(.diff-file-header) {
  padding: 8px 10px;
}
</style>
