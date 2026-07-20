<template>
  <div
    class="prompt-shell"
    :class="{ 'prompt-shell--compact': compact, 'prompt-shell--dragging': draggingFiles }"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent="onDragOver"
    @dragleave.prevent="onDragLeave"
    @drop.prevent="onDrop"
    @paste="onPaste"
  >
    <div v-if="title || showBadge" class="prompt-shell__header">
      <div class="text-subtitle2 text-weight-bold">{{ title }}</div>
      <q-badge
        v-if="showBadge"
        rounded
        class="attachment-count-badge"
        :label="attachmentCount > 0 ? `${attachmentCount} 个附件` : '可附加上下文'"
      />
    </div>

    <div v-if="showAttachmentZone" class="attachment-zone">
      <div class="text-caption text-muted">附件</div>
      <div v-if="attachmentCount > 0" class="attachment-list">
        <q-chip
          v-for="file in files"
          :key="`${file.name}-${file.size}-${file.lastModified}`"
          removable
          square
          :clickable="!disabled && canPreview(file)"
          :disable="disabled"
          class="attachment-chip"
          :icon="fileIcon(file)"
          @click="openPreview(file)"
          @remove="removeFile(file)"
        >
          <span class="ellipsis">{{ file.name }}</span>
          <q-icon v-if="canPreview(file)" name="visibility" class="q-ml-sm" />
        </q-chip>
        <q-chip
          v-for="artifact in artifacts"
          :key="artifact.id"
          removable
          square
          :disable="disabled"
          class="attachment-chip"
          :icon="artifactIcon(artifact)"
          @remove="removeArtifact(artifact)"
        >
          <span class="ellipsis">{{ artifact.logicalPath || artifact.filename }}</span>
        </q-chip>
      </div>
      <div v-else class="text-caption text-muted attachment-empty">松开添加附件</div>
    </div>

    <q-input
      v-model.trim="promptModel"
      autogrow
      borderless
      type="textarea"
      class="prompt-input"
      :placeholder="placeholder"
      :disable="disabled"
      @keydown.shift.enter.prevent="emit('submit')"
    />

    <div class="prompt-toolbar">
      <q-file
        v-model="filesModel"
        borderless
        dense
        multiple
        append
        display-value=""
        aria-label="添加附件"
        class="toolbar-file-picker"
        :disable="disabled"
      >
        <template #prepend>
          <q-icon name="attach_file" />
        </template>
        <q-tooltip>添加附件</q-tooltip>
      </q-file>
      <PromptConfigControls
        v-if="!forceConfigMenu && (!compact || !$q.screen.lt.md)"
        :model="model"
        :effort="effort"
        :permission="permission"
        :fast="fast"
        :disabled="disabled"
        :readonly-config="readonlyConfig"
        @update:model="emit('update:model', $event)"
        @update:effort="emit('update:effort', $event)"
        @update:permission="emit('update:permission', $event)"
        @update:fast="emit('update:fast', $event)"
      />
      <q-btn
        v-else
        flat
        round
        class="app-icon-btn prompt-config-trigger"
        icon="tune"
        aria-label="运行参数"
        :disable="disabled"
      >
        <q-menu class="prompt-config-menu" anchor="top right" self="bottom right">
          <PromptConfigControls
            class="prompt-config-controls--stacked"
            :model="model"
            :effort="effort"
            :permission="permission"
            :fast="fast"
            :disabled="disabled"
            :readonly-config="readonlyConfig"
            @update:model="emit('update:model', $event)"
            @update:effort="emit('update:effort', $event)"
            @update:permission="emit('update:permission', $event)"
            @update:fast="emit('update:fast', $event)"
          />
        </q-menu>
      </q-btn>
      <q-space />
      <slot name="actions" />
    </div>

    <q-dialog v-model="previewOpen" :maximized="$q.screen.lt.sm">
      <q-card class="attachment-preview-card app-content-dialog">
        <q-card-section class="row items-center q-pb-sm">
          <div class="text-subtitle2 text-weight-bold ellipsis">{{ previewName }}</div>
          <q-space />
          <q-btn
            flat
            round
            dense
            class="app-icon-btn"
            icon="close"
            aria-label="关闭预览"
            @click="closePreview"
          >
            <q-tooltip>关闭预览</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />
        <q-card-section class="attachment-preview-body">
          <img
            v-if="previewKind === 'image'"
            :src="previewUrl"
            alt=""
            class="attachment-preview-media"
          />
          <video
            v-else-if="previewKind === 'video'"
            :src="previewUrl"
            class="attachment-preview-media"
            controls
          />
        </q-card-section>
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import { useQuasar } from 'quasar';

import PromptConfigControls from '@/components/PromptConfigControls.vue';
import { filesFromTransfer } from '@/services/promptAttachments';
import type { SessionFile } from '@/services/sessionFiles';

const props = withDefaults(
  defineProps<{
    prompt: string;
    files: File[];
    artifacts?: SessionFile[];
    model: string;
    effort: string;
    permission: string;
    fast: boolean;
    title?: string;
    placeholder?: string;
    disabled?: boolean;
    compact?: boolean;
    showBadge?: boolean;
    forceConfigMenu?: boolean;
    readonlyConfig?: boolean;
  }>(),
  {
    title: '',
    placeholder: '描述你希望 Codex 完成的任务',
    disabled: false,
    compact: false,
    showBadge: true,
    forceConfigMenu: false,
    readonlyConfig: false,
    artifacts: () => [],
  },
);

const emit = defineEmits<{
  'update:prompt': [value: string];
  'update:files': [value: File[]];
  'update:artifacts': [value: SessionFile[]];
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
  'update:fast': [value: boolean];
  submit: [];
}>();

const $q = useQuasar();
const previewOpen = ref(false);
const previewName = ref('');
const previewKind = ref<'image' | 'video' | ''>('');
const previewUrl = ref('');
const draggingFiles = ref(false);
const dragDepth = ref(0);

const promptModel = computed({
  get: () => props.prompt,
  set: (value: string) => emit('update:prompt', value),
});
const filesModel = computed({
  get: () => props.files,
  set: (value: File[] | File | null) =>
    emit('update:files', Array.isArray(value) ? value : value ? [value] : []),
});
const attachmentCount = computed(() => props.files.length + props.artifacts.length);
const showAttachmentZone = computed(() => attachmentCount.value > 0 || draggingFiles.value);

function fileIcon(file: File) {
  if (file.type.startsWith('image/')) return 'image';
  if (file.type.startsWith('video/')) return 'movie';
  return 'description';
}

function artifactIcon(artifact: SessionFile) {
  if (artifact.artifactKind === 'image') return 'image';
  if (artifact.artifactKind === 'video') return 'movie';
  if (artifact.artifactKind === 'audio') return 'audio_file';
  if (artifact.artifactKind === 'archive') return 'folder_zip';
  if (artifact.artifactKind === 'pdf') return 'picture_as_pdf';
  return 'description';
}

function canPreview(file: File) {
  return file.type.startsWith('image/') || file.type.startsWith('video/');
}

function openPreview(file: File) {
  if (props.disabled || !canPreview(file)) return;
  revokePreviewUrl();
  previewName.value = file.name;
  previewKind.value = file.type.startsWith('image/') ? 'image' : 'video';
  previewUrl.value = URL.createObjectURL(file);
  previewOpen.value = true;
}

function closePreview() {
  previewOpen.value = false;
  previewName.value = '';
  previewKind.value = '';
  revokePreviewUrl();
}

function revokePreviewUrl() {
  if (!previewUrl.value) return;
  URL.revokeObjectURL(previewUrl.value);
  previewUrl.value = '';
}

function removeFile(file: File) {
  if (props.disabled) return;
  emit(
    'update:files',
    props.files.filter((item) => item !== file),
  );
}

function removeArtifact(artifact: SessionFile) {
  if (props.disabled) return;
  emit(
    'update:artifacts',
    props.artifacts.filter((item) => item.id !== artifact.id),
  );
}

function onDragEnter(event: DragEvent) {
  if (props.disabled || !hasDraggedFiles(event)) return;
  dragDepth.value += 1;
  draggingFiles.value = true;
}

function onDragOver(event: DragEvent) {
  if (props.disabled || !hasDraggedFiles(event)) return;
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'copy';
  }
  draggingFiles.value = true;
}

function onDragLeave(event: DragEvent) {
  if (props.disabled || !hasDraggedFiles(event)) return;
  dragDepth.value = Math.max(0, dragDepth.value - 1);
  if (dragDepth.value === 0) {
    draggingFiles.value = false;
  }
}

function onDrop(event: DragEvent) {
  dragDepth.value = 0;
  draggingFiles.value = false;
  if (props.disabled) return;
  appendFiles(filesFromTransfer(event.dataTransfer));
}

function onPaste(event: ClipboardEvent) {
  if (props.disabled) return;
  appendFiles(filesFromTransfer(event.clipboardData));
}

function hasDraggedFiles(event: DragEvent) {
  return Array.from(event.dataTransfer?.types ?? []).includes('Files');
}

function appendFiles(nextFiles: File[]) {
  if (nextFiles.length === 0) return;
  emit('update:files', [...props.files, ...nextFiles]);
}

onBeforeUnmount(revokePreviewUrl);
</script>
