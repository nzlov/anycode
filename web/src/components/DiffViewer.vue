<template>
  <div class="diff-viewer">
    <q-card v-for="file in visibleFiles" :key="file.path" flat bordered class="diff-file-card">
      <q-card-section class="diff-file-header">
        <div
          class="file-title"
          :class="{ 'file-title--collapsible': collapsible }"
          :role="collapsible ? 'button' : undefined"
          :tabindex="collapsible ? 0 : undefined"
          :aria-expanded="collapsible ? !isCollapsed(file.path) : undefined"
          @click="toggleCollapse(file.path)"
          @keydown.enter.prevent="toggleCollapse(file.path)"
          @keydown.space.prevent="toggleCollapse(file.path)"
        >
          <q-spinner v-if="isLoading(file.path)" color="primary" size="18px" />
          <q-icon
            v-else-if="collapsible"
            :name="isCollapsed(file.path) ? 'chevron_right' : 'expand_more'"
          />
          <q-icon :name="fileIcon(file.status)" :color="fileColor(file.status)" />
          <slot name="file-title" :file="file">
            <span>{{ file.path }}</span>
          </slot>
        </div>
        <div class="row items-center q-gutter-sm" @click.stop @keydown.stop>
          <q-badge outline color="positive" :label="`+${file.additions}`" />
          <q-badge outline color="negative" :label="`-${file.deletions}`" />
          <q-badge outline :color="fileColor(file.status)" :label="file.status" />
        </div>
      </q-card-section>
      <q-separator v-if="!isCollapsed(file.path) && fileDiffFor(file.path)" />
      <q-card-section v-if="!isCollapsed(file.path) && fileDiffFor(file.path)" class="diff-code">
        <template
          v-for="hunk in fileDiffFor(file.path)?.hunks ?? []"
          :key="`${file.path}:${hunk.id}`"
        >
          <div v-if="hunk.canExpandBefore" class="diff-expand-row">
            <q-btn
              flat
              dense
              no-caps
              icon="expand_less"
              label="向上展开 20 行"
              @click="$emit('expand', file.path, 'before')"
            />
          </div>
          <div
            v-for="line in hunk.lines"
            :key="`${file.path}:${hunk.id}:${line.id}`"
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
              @click="$emit('expand', file.path, 'after')"
            />
          </div>
        </template>
      </q-card-section>
    </q-card>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import type { DiffFile, DiffLineKind, FileDiff } from '@/services/diff';

const props = withDefaults(
  defineProps<{
    fileDiffs: FileDiff[];
    files?: DiffFile[];
    collapsible?: boolean;
    collapsedPaths?: string[];
    loadingPaths?: string[];
  }>(),
  {
    files: () => [],
    collapsible: false,
    collapsedPaths: () => [],
    loadingPaths: () => [],
  },
);

const emit = defineEmits<{
  expand: [filePath: string, direction: 'before' | 'after'];
  'toggle-collapse': [filePath: string];
}>();

const visibleFiles = computed(() =>
  props.files.length > 0 ? props.files : props.fileDiffs.map((fileDiff) => fileDiff.file),
);
const fileDiffsByPath = computed(
  () => new Map(props.fileDiffs.map((fileDiff) => [fileDiff.file.path, fileDiff])),
);

function fileDiffFor(filePath: string) {
  return fileDiffsByPath.value.get(filePath);
}

function isLoading(filePath: string) {
  return props.loadingPaths.includes(filePath);
}

function isCollapsed(filePath: string) {
  return props.collapsible && props.collapsedPaths.includes(filePath);
}

function toggleCollapse(filePath: string) {
  if (props.collapsible) emit('toggle-collapse', filePath);
}

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
  overflow: visible;
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.diff-file-header {
  position: sticky;
  top: 0;
  z-index: 1;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 16px;
  background: var(--ac-surface);
}

.file-title {
  flex: 1 1 auto;
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.file-title--collapsible {
  cursor: pointer;
}

.file-title--collapsible:focus-visible {
  border-radius: 4px;
  outline: 2px solid var(--q-primary);
  outline-offset: 3px;
}

.file-title span {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.diff-code {
  overflow-x: auto;
  padding: 0;
  border-radius: 0 0 var(--ac-radius) var(--ac-radius);
  background: var(--ac-diff-bg);
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
  border-right: 1px solid var(--ac-border);
  color: var(--ac-diff-gutter);
  text-align: right;
  user-select: none;
}

.line-context {
  color: var(--ac-diff-context);
}

.line-add {
  color: var(--ac-diff-added-text);
  background: var(--ac-diff-added-bg);
}

.line-delete {
  color: var(--ac-diff-removed-text);
  background: var(--ac-diff-removed-bg);
}

.line-header,
.diff-expand-row {
  color: var(--ac-diff-hunk-text);
  background: var(--ac-diff-hunk-bg);
}

.diff-expand-row {
  min-width: max-content;
  padding: 4px 112px;
}

.diff-expand-row :deep(.q-btn) {
  color: var(--ac-diff-hunk-text);
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
