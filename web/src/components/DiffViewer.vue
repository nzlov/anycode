<template>
  <div class="diff-viewer">
    <q-card
      v-for="fileDiff in fileDiffs"
      :key="fileDiff.file.path"
      flat
      bordered
      class="diff-file-card"
    >
      <q-card-section class="diff-file-header">
        <div class="file-title">
          <q-icon :name="fileIcon(fileDiff.file.status)" :color="fileColor(fileDiff.file.status)" />
          <slot name="file-title" :file="fileDiff.file">
            <span>{{ fileDiff.file.path }}</span>
          </slot>
        </div>
        <div class="row items-center q-gutter-sm">
          <q-badge outline color="positive" :label="`+${fileDiff.file.additions}`" />
          <q-badge outline color="negative" :label="`-${fileDiff.file.deletions}`" />
          <q-badge outline :color="fileColor(fileDiff.file.status)" :label="fileDiff.file.status" />
        </div>
      </q-card-section>
      <q-separator />
      <q-card-section class="diff-code">
        <template v-for="hunk in fileDiff.hunks" :key="`${fileDiff.file.path}:${hunk.id}`">
          <div v-if="hunk.canExpandBefore" class="diff-expand-row">
            <q-btn
              flat
              dense
              no-caps
              icon="expand_less"
              label="向上展开 20 行"
              @click="$emit('expand', fileDiff.file.path, 'before')"
            />
          </div>
          <div
            v-for="line in hunk.lines"
            :key="`${fileDiff.file.path}:${hunk.id}:${line.id}`"
            class="diff-line"
            :class="lineClass(line.kind)"
          >
            <span class="line-number">{{ line.oldLine ?? '' }}</span>
            <span class="line-number">{{ line.newLine ?? '' }}</span>
            <pre>{{ line.content }}</pre>
          </div>
          <div v-if="hunk.canExpandAfter" class="diff-expand-row">
            <q-btn
              flat
              dense
              no-caps
              icon="expand_more"
              label="向下展开 20 行"
              @click="$emit('expand', fileDiff.file.path, 'after')"
            />
          </div>
        </template>
      </q-card-section>
    </q-card>
  </div>
</template>

<script setup lang="ts">
import type { DiffFile, DiffLineKind, FileDiff } from '@/services/diff';

defineProps<{
  fileDiffs: FileDiff[];
}>();

defineEmits<{
  expand: [filePath: string, direction: 'before' | 'after'];
}>();

function fileIcon(status: DiffFile['status']) {
  if (status === 'added') return 'add_circle';
  if (status === 'deleted') return 'remove_circle';
  if (status === 'renamed') return 'drive_file_rename_outline';
  return 'edit';
}

function fileColor(status: DiffFile['status']) {
  if (status === 'added') return 'positive';
  if (status === 'deleted') return 'negative';
  if (status === 'renamed') return 'warning';
  return 'primary';
}

function lineClass(kind: DiffLineKind) {
  return {
    'line-add': kind === 'add',
    'line-delete': kind === 'delete',
    'line-header': kind === 'header',
    'line-context': kind === 'context',
  };
}
</script>

<style scoped>
.diff-viewer {
  display: grid;
  gap: 12px;
}

.diff-file-card {
  overflow: hidden;
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.diff-file-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
}

.file-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.file-title span {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.diff-code {
  overflow-x: auto;
  padding: 0;
  background: #0f172a;
}

.diff-line {
  display: grid;
  grid-template-columns: 56px 56px minmax(max-content, 1fr);
  min-width: max-content;
  font-family: ui-monospace, SFMono-Regular, Consolas, 'Liberation Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
}

.diff-line pre {
  margin: 0;
  padding: 4px 16px;
  white-space: pre;
}

.line-number {
  padding: 4px 8px;
  border-right: 1px solid rgba(148, 163, 184, 0.18);
  color: #94a3b8;
  text-align: right;
  user-select: none;
}

.line-context {
  color: #dbeafe;
}

.line-add {
  color: #bbf7d0;
  background: rgba(22, 101, 52, 0.32);
}

.line-delete {
  color: #fecaca;
  background: rgba(127, 29, 29, 0.32);
}

.line-header,
.diff-expand-row {
  color: #bfdbfe;
  background: rgba(30, 64, 175, 0.3);
}

.diff-expand-row {
  min-width: max-content;
  padding: 4px 112px;
}

.diff-expand-row :deep(.q-btn) {
  color: #bfdbfe;
}

@media (max-width: 720px) {
  .diff-line {
    grid-template-columns: 44px 44px minmax(max-content, 1fr);
    font-size: 11px;
  }

  .diff-expand-row {
    padding-left: 88px;
  }
}
</style>
