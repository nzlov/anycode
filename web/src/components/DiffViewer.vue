<template>
  <div class="diff-viewer">
    <q-card
      v-for="file in visibleFiles"
      :key="file.path"
      flat
      bordered
      class="diff-file-card"
      :class="{ 'diff-file-card--headerless': !showFileHeaders }"
    >
      <q-card-section v-if="showFileHeaders" class="diff-file-header">
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
        <div class="diff-file-meta row items-center q-gutter-sm" @click.stop @keydown.stop>
          <q-badge outline color="positive" :label="`+${file.additions}`" />
          <q-badge outline color="negative" :label="`-${file.deletions}`" />
          <q-badge outline :color="fileColor(file.status)" :label="file.status" />
        </div>
      </q-card-section>
      <q-separator
        v-if="
          showFileHeaders &&
          !isCollapsed(file.path) &&
          (fileDiffFor(file.path) || mediaPreviewFor(file.path))
        "
      />
      <q-card-section
        v-if="!isCollapsed(file.path) && mediaPreviewFor(file.path)"
        class="diff-media"
      >
        <DiffMediaPreview
          :session-id="mediaPreviewFor(file.path)!.sessionId"
          :file-path="mediaPreviewFor(file.path)!.filePath"
          :status="file.status"
          :kind="mediaPreviewFor(file.path)!.kind"
          :display-annotation="displayAnnotation"
        />
      </q-card-section>
      <PreviewAnnotator
        v-else-if="!isCollapsed(file.path) && fileDiffFor(file.path)"
        mode="text"
        :source="`变更 ${file.path}`"
        :session-id="annotationSessionIds[file.path] ?? ''"
        :content-key="fileDiffFor(file.path)"
        :file-references="[
          { kind: 'diff', filePath: file.path, version: 'old' },
          { kind: 'diff', filePath: file.path, version: 'new' },
        ]"
        :enabled="!displayAnnotation"
        :display-annotations="displayAnnotation?.marks ?? []"
      >
        <DiffTextContent
          :file-diff="fileDiffFor(file.path)!"
          :initial-source="Boolean(displayAnnotation)"
          @expand="$emit('expand', file.path, $event)"
        />
      </PreviewAnnotator>
    </q-card>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import DiffMediaPreview from '@/components/DiffMediaPreview.vue';
import DiffTextContent from '@/components/DiffTextContent.vue';
import PreviewAnnotator from '@/components/PreviewAnnotator.vue';
import type { DiffFile, FileDiff } from '@/services/diff';
import type { DiffMediaPreviewTarget } from '@/services/diffMediaModel';
import type { PreviewAnnotationAttachment } from '@/services/previewAnnotations';

const props = withDefaults(
  defineProps<{
    fileDiffs: FileDiff[];
    files?: DiffFile[];
    collapsible?: boolean;
    showFileHeaders?: boolean;
    collapsedPaths?: string[];
    loadingPaths?: string[];
    mediaPreviews?: Record<string, DiffMediaPreviewTarget>;
    annotationSessionIds?: Record<string, string>;
    displayAnnotation?: PreviewAnnotationAttachment | null;
  }>(),
  {
    files: () => [],
    collapsible: false,
    showFileHeaders: true,
    collapsedPaths: () => [],
    loadingPaths: () => [],
    mediaPreviews: () => ({}),
    annotationSessionIds: () => ({}),
    displayAnnotation: null,
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

function mediaPreviewFor(filePath: string) {
  return props.mediaPreviews[filePath];
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
</script>

<style scoped>
.diff-viewer {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: 12px;
}

.diff-file-card {
  container-type: inline-size;
  content-visibility: auto;
  min-width: 0;
  overflow: visible;
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
  contain-intrinsic-size: auto 240px;
}

.diff-file-card--headerless {
  overflow: clip;
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

@container (max-width: 840px) {
  .diff-file-header {
    flex-wrap: wrap;
  }

  .diff-file-meta {
    flex: 0 0 100%;
    justify-content: flex-end;
    margin-left: auto;
  }
}
</style>
