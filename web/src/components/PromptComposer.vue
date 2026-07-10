<template>
  <div
    class="prompt-shell"
    :class="{ 'prompt-shell--compact': compact, 'prompt-shell--dragging': draggingFiles }"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent="onDragOver"
    @dragleave.prevent="onDragLeave"
    @drop.prevent="onDrop"
  >
    <div v-if="title || showBadge" class="prompt-shell__header">
      <div class="text-subtitle2 text-weight-bold">{{ title }}</div>
      <q-badge
        v-if="showBadge"
        rounded
        :color="files.length > 0 ? 'cyan-1' : 'indigo-1'"
        :text-color="files.length > 0 ? 'cyan-9' : 'indigo-9'"
        :label="files.length > 0 ? `${files.length} 个附件` : '可附加上下文'"
      />
    </div>

    <div v-if="showAttachmentZone" class="attachment-zone">
      <div class="text-caption text-muted">附件</div>
      <div v-if="files.length > 0" class="attachment-list">
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
      <div v-if="readonlyConfig" class="prompt-config-chip">
        <q-icon :name="permissionIcon" />
        <span>{{ permissionLabel }}</span>
        <q-tooltip>运行权限</q-tooltip>
      </div>
      <q-select
        v-else
        v-model="permissionModel"
        dense
        borderless
        emit-value
        map-options
        class="compact-select"
        :disable="disabled"
        :options="permissionModeOptions"
      >
        <template #prepend>
          <q-icon :name="permissionIcon" />
        </template>
        <q-tooltip>运行权限</q-tooltip>
      </q-select>
      <div v-if="readonlyConfig" class="prompt-config-chip">
        <q-icon name="smart_toy" />
        <span>{{ modelLabel }}</span>
        <q-tooltip>Codex 模型</q-tooltip>
      </div>
      <q-select
        v-else
        v-model="modelModel"
        dense
        borderless
        emit-value
        map-options
        class="compact-select"
        :disable="disabled"
        :options="modelOptions"
      >
        <template #prepend>
          <q-icon name="smart_toy" />
        </template>
        <q-tooltip>Codex 模型</q-tooltip>
      </q-select>
      <div v-if="readonlyConfig" class="prompt-config-chip">
        <q-icon name="psychology" />
        <span>{{ effortLabel }}</span>
        <q-tooltip>思考强度</q-tooltip>
      </div>
      <q-select
        v-else
        v-model="effortModel"
        dense
        borderless
        emit-value
        map-options
        class="compact-select"
        :disable="disabled"
        :options="reasoningEffortOptions"
      >
        <template #prepend>
          <q-icon name="psychology" />
        </template>
        <q-tooltip>思考强度</q-tooltip>
      </q-select>
      <q-space />
      <slot name="actions" />
    </div>

    <q-dialog v-model="previewOpen">
      <q-card class="attachment-preview-card">
        <q-card-section class="row items-center q-pb-sm">
          <div class="text-subtitle2 text-weight-bold ellipsis">{{ previewName }}</div>
          <q-space />
          <q-btn flat round dense icon="close" aria-label="关闭预览" @click="closePreview">
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
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import {
  type CodexModelOption,
  codexModelLabel,
  defaultReasoningEffortForModel,
  normalizeCodexModel,
  normalizeReasoningEffort,
  permissionModeLabel,
  permissionModeOptions,
  reasoningEffortLabel,
  reasoningEffortOptionsForModel,
} from '@/components/promptOptions';

const props = withDefaults(
  defineProps<{
    prompt: string;
    files: File[];
    model: string;
    effort: string;
    permission: string;
    title?: string;
    placeholder?: string;
    disabled?: boolean;
    compact?: boolean;
    showBadge?: boolean;
    readonlyConfig?: boolean;
    modelOptions?: CodexModelOption[];
  }>(),
  {
    title: '',
    placeholder: '描述你希望 Codex 完成的任务',
    disabled: false,
    compact: false,
    showBadge: true,
    readonlyConfig: false,
    modelOptions: () => [],
  },
);

const emit = defineEmits<{
  'update:prompt': [value: string];
  'update:files': [value: File[]];
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
}>();

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
  set: (value: File[] | File | null) => emit('update:files', Array.isArray(value) ? value : value ? [value] : []),
});
const modelModel = computed({
  get: () => props.model,
  set: (value: string) => {
    const nextModel = normalizeCodexModel(props.modelOptions, value);
    emit('update:model', nextModel);
    const nextEffort = defaultReasoningEffortForModel(props.modelOptions, nextModel);
    if (nextEffort !== props.effort) {
      emit('update:effort', nextEffort);
    }
  },
});
const effortModel = computed({
  get: () => props.effort,
  set: (value: string) => emit('update:effort', normalizeReasoningEffort(props.modelOptions, props.model, value)),
});
const permissionModel = computed({
  get: () => props.permission,
  set: (value: string) => emit('update:permission', value),
});
const permissionIcon = computed(
  () => permissionModeOptions.find((option) => option.value === props.permission)?.icon ?? 'edit_note',
);
const permissionLabel = computed(() => permissionModeLabel(props.permission));
const modelLabel = computed(() => codexModelLabel(props.modelOptions, props.model));
const effortLabel = computed(() => reasoningEffortLabel(props.modelOptions, props.model, props.effort));
const reasoningEffortOptions = computed(() => reasoningEffortOptionsForModel(props.modelOptions, props.model));
const showAttachmentZone = computed(() => props.files.length > 0 || draggingFiles.value);

watch(
  () => [props.model, props.modelOptions] as const,
  ([value]) => {
    const nextModel = normalizeCodexModel(props.modelOptions, value);
    if (nextModel !== value) {
      emit('update:model', nextModel);
      return;
    }
    const nextEffort = normalizeReasoningEffort(props.modelOptions, nextModel, props.effort);
    if (nextEffort !== props.effort) {
      emit('update:effort', nextEffort);
    }
  },
  { immediate: true },
);

watch(
  () => [props.effort, props.modelOptions] as const,
  ([value]) => {
    const nextEffort = normalizeReasoningEffort(props.modelOptions, props.model, value);
    if (nextEffort !== value) {
      emit('update:effort', nextEffort);
    }
  },
  { immediate: true },
);

function fileIcon(file: File) {
  if (file.type.startsWith('image/')) return 'image';
  if (file.type.startsWith('video/')) return 'movie';
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
  appendFiles(draggedFiles(event));
}

function hasDraggedFiles(event: DragEvent) {
  return Array.from(event.dataTransfer?.types ?? []).includes('Files');
}

function appendFiles(nextFiles: File[]) {
  if (nextFiles.length === 0) return;
  emit('update:files', [...props.files, ...nextFiles]);
}

function draggedFiles(event: DragEvent) {
  const dataTransfer = event.dataTransfer;
  if (!dataTransfer) return [];
  const files = Array.from(dataTransfer.files ?? []);
  if (files.length > 0) return files;
  return Array.from(dataTransfer.items ?? [])
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file));
}

onBeforeUnmount(revokePreviewUrl);
</script>
