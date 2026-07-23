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
    <div v-if="isCollapsed" class="prompt-shell__collapsed">
      <q-btn
        flat
        class="prompt-shell__expand"
        icon="keyboard"
        aria-label="展开提示词"
        @click="emit('update:collapsed', false)"
      >
        <q-tooltip>展开提示词</q-tooltip>
      </q-btn>
      <slot name="quick-actions" :collapsed="true" />
      <slot name="actions" />
    </div>

    <div v-if="!isCollapsed && (title || showBadge)" class="prompt-shell__header">
      <div class="text-subtitle2 text-weight-bold">{{ title }}</div>
      <q-badge
        v-if="showBadge"
        rounded
        class="attachment-count-badge"
        :label="attachmentCount > 0 ? `${attachmentCount} 个附件` : '可附加上下文'"
      />
    </div>

    <div v-if="!isCollapsed && showAttachmentZone" class="attachment-zone">
      <div class="text-caption text-muted">附件</div>
      <div v-if="attachmentCount > 0" class="attachment-list">
        <template v-for="file in files" :key="`${file.name}-${file.size}-${file.lastModified}`">
          <div v-if="isImageFile(file)" class="attachment-image-item">
            <div class="attachment-image-preview">
              <button
                type="button"
                class="attachment-image-trigger"
                :disabled="disabled"
                :aria-label="`预览图片 ${file.name}`"
                @click="openPreview(file)"
              >
                <img :src="fileThumbnailUrl(file)" alt="" class="attachment-thumbnail" />
                <q-tooltip
                  v-if="!previewOpen"
                  anchor="top middle"
                  self="bottom middle"
                  :offset="[0, 8]"
                  :delay="200"
                  class="attachment-image-tooltip"
                >
                  <img
                    :src="fileThumbnailUrl(file)"
                    :alt="file.name"
                    class="attachment-hover-preview"
                  />
                </q-tooltip>
              </button>
              <q-btn
                flat
                round
                dense
                icon="close"
                class="attachment-image-remove"
                :disable="disabled"
                :aria-label="`移除图片 ${file.name}`"
                @click.stop="removeFile(file)"
              >
                <q-tooltip>移除图片</q-tooltip>
              </q-btn>
            </div>
            <span class="attachment-image-name ellipsis" :title="file.name">{{ file.name }}</span>
          </div>
          <q-chip
            v-else
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
        </template>
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

    <div v-if="!isCollapsed" class="prompt-input-wrap">
      <q-input
        ref="promptInputRef"
        v-model="promptModel"
        autogrow
        borderless
        type="textarea"
        class="prompt-input"
        :placeholder="placeholder"
        :disable="disabled"
        :aria-expanded="completionOpen"
        :aria-controls="completionOpen ? completionListId : undefined"
        :aria-activedescendant="activeCompletionId"
        @update:model-value="queuePromptCompletionRefresh"
        @focus="queuePromptCompletionRefresh"
        @click="queuePromptCompletionRefresh"
        @keyup="onPromptKeyup"
        @keydown="onPromptKeydown"
        @keydown.shift.enter.prevent="emit('submit')"
        @blur="onPromptBlur"
      />

      <div
        v-if="completionOpen"
        :id="completionListId"
        class="prompt-completion"
        role="listbox"
        :aria-label="completionHeading"
      >
        <div class="prompt-completion__header">
          <q-icon :name="completionRange?.kind === 'command' ? 'terminal' : 'description'" />
          <span>{{ completionHeading }}</span>
          <q-space />
          <q-spinner v-if="completionLoading" color="primary" size="16px" />
        </div>
        <q-list v-if="completionItems.length" dense class="prompt-completion__list">
          <q-item
            v-for="(item, index) in completionItems"
            :id="`${completionListId}-option-${index}`"
            :key="item.key"
            clickable
            role="option"
            :active="index === activeCompletionIndex"
            :aria-selected="index === activeCompletionIndex"
            active-class="prompt-completion__item--active"
            class="prompt-completion__item"
            @mouseenter="activeCompletionIndex = index"
            @mousedown.prevent
            @click="selectCompletion(index)"
          >
            <q-item-section avatar>
              <q-icon :name="item.kind === 'command' ? 'terminal' : 'insert_drive_file'" />
            </q-item-section>
            <q-item-section>
              <q-item-label v-if="item.kind === 'command'" class="prompt-completion__value">
                {{ item.command.name }}
              </q-item-label>
              <q-item-label v-else class="prompt-completion__value prompt-completion__path">
                <span
                  v-for="(segment, segmentIndex) in fileMatchSegments(item.file)"
                  :key="segmentIndex"
                  :class="{ 'prompt-completion__match': segment.matched }"
                  >{{ segment.text }}</span
                >
              </q-item-label>
              <q-item-label v-if="item.kind === 'command'" caption>
                {{ item.command.description }}
              </q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
        <div v-else class="prompt-completion__empty">
          <template v-if="completionLoading">正在匹配</template>
          <template v-else-if="completionError">加载失败</template>
          <template v-else>无匹配项</template>
        </div>
      </div>
    </div>

    <div v-if="!isCollapsed" class="prompt-toolbar">
      <q-btn
        v-if="collapsible"
        flat
        round
        class="app-icon-btn prompt-shell__collapse"
        icon="keyboard_hide"
        aria-label="收起提示词"
        @click="emit('update:collapsed', true)"
      >
        <q-tooltip>收起提示词</q-tooltip>
      </q-btn>
      <q-btn
        flat
        round
        icon="attach_file"
        aria-label="添加附件"
        class="app-icon-btn toolbar-file-picker"
        :disable="disabled"
        @click="filePickerRef?.pickFiles($event)"
      >
        <q-tooltip>添加附件</q-tooltip>
      </q-btn>
      <q-file
        ref="filePickerRef"
        v-model="filesModel"
        multiple
        append
        class="hidden"
        :disable="disabled"
      />
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
      <slot name="quick-actions" :collapsed="false" />
      <slot name="actions" />
    </div>

    <q-dialog v-model="previewOpen">
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
import { computed, nextTick, onBeforeUnmount, reactive, ref, useId, watch } from 'vue';
import { useQuasar, type QFile, type QInput } from 'quasar';

import PromptConfigControls from '@/components/PromptConfigControls.vue';
import { filesFromTransfer } from '@/services/promptAttachments';
import {
  activePromptCompletion,
  applyPromptCompletion,
  filterSlashCommands,
  formatFileMention,
  promptMatchSegments,
  type PromptCompletionRange,
} from '@/services/promptCompletionText.js';
import {
  listCodexSlashCommands,
  searchPromptFiles,
  type PromptFileMatch,
  type PromptSlashCommand,
} from '@/services/promptCompletions';
import type { SessionFile } from '@/services/sessionFiles';
import type { PromptMention } from '@/services/sessions';

const props = withDefaults(
  defineProps<{
    prompt: string;
    files: File[];
    artifacts?: SessionFile[];
    mentions?: PromptMention[];
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
    collapsible?: boolean;
    collapsed?: boolean;
    completionProjectId?: string;
    completionSessionId?: string;
    completionHasThread?: boolean;
  }>(),
  {
    title: '',
    placeholder: '描述你希望 Codex 完成的任务',
    disabled: false,
    compact: false,
    showBadge: true,
    forceConfigMenu: false,
    readonlyConfig: false,
    collapsible: false,
    collapsed: false,
    completionProjectId: '',
    completionSessionId: '',
    completionHasThread: false,
    artifacts: () => [],
    mentions: () => [],
  },
);

const emit = defineEmits<{
  'update:prompt': [value: string];
  'update:files': [value: File[]];
  'update:artifacts': [value: SessionFile[]];
  'update:mentions': [value: PromptMention[]];
  'update:model': [value: string];
  'update:effort': [value: string];
  'update:permission': [value: string];
  'update:fast': [value: boolean];
  'update:collapsed': [value: boolean];
  submit: [];
}>();

const $q = useQuasar();
const previewOpen = ref(false);
const previewName = ref('');
const previewKind = ref<'image' | 'video' | ''>('');
const previewUrl = ref('');
const draggingFiles = ref(false);
const dragDepth = ref(0);
const fileThumbnailUrls = reactive(new Map<File, string>());
const filePickerRef = ref<QFile | null>(null);
const promptInputRef = ref<QInput | null>(null);
const completionListId = useId();
const completionRange = ref<PromptCompletionRange | null>(null);
const slashCommands = ref<PromptSlashCommand[]>([]);
const fileMatches = ref<PromptFileMatch[]>([]);
const slashCommandsLoaded = ref(false);
const commandLoading = ref(false);
const fileLoading = ref(false);
const completionError = ref(false);
const activeCompletionIndex = ref(0);
let fileSearchTimer: ReturnType<typeof setTimeout> | null = null;
let fileSearchGeneration = 0;
let blurTimer: ReturnType<typeof setTimeout> | null = null;

const promptModel = computed({
  get: () => props.prompt,
  set: (value: string) => {
    emit('update:prompt', value);
    emit(
      'update:mentions',
      props.mentions.filter((mention) => value.includes(formatFileMention(mention.path))),
    );
  },
});
const filesModel = computed({
  get: () => props.files,
  set: (value: File[] | File | null) =>
    emit('update:files', Array.isArray(value) ? value : value ? [value] : []),
});
const attachmentCount = computed(() => props.files.length + props.artifacts.length);
const showAttachmentZone = computed(() => attachmentCount.value > 0 || draggingFiles.value);
const isCollapsed = computed(() => props.collapsible && props.collapsed);
const completionScopeReady = computed(
  () => Boolean(props.completionProjectId) !== Boolean(props.completionSessionId),
);
const completionItems = computed(() => {
  const range = completionRange.value;
  if (!range) return [];
  if (range.kind === 'command') {
    return filterSlashCommands(slashCommands.value, range.query, props.completionHasThread).map(
      (command) => ({ kind: 'command' as const, key: command.name, command }),
    );
  }
  return fileMatches.value.map((file) => ({ kind: 'file' as const, key: file.path, file }));
});
const completionOpen = computed(
  () =>
    Boolean(completionRange.value) &&
    !props.disabled &&
    !isCollapsed.value &&
    (completionRange.value?.kind === 'command' || completionScopeReady.value),
);
const completionLoading = computed(() =>
  completionRange.value?.kind === 'command' ? commandLoading.value : fileLoading.value,
);
const completionHeading = computed(() =>
  completionRange.value?.kind === 'command' ? 'Codex 指令' : '项目文件',
);
const activeCompletionId = computed(() =>
  completionOpen.value && completionItems.value.length > 0
    ? `${completionListId}-option-${activeCompletionIndex.value}`
    : undefined,
);

function fileIcon(file: File) {
  if (file.type.startsWith('video/')) return 'movie';
  return 'description';
}

function isImageFile(file: File) {
  return file.type.startsWith('image/');
}

function fileThumbnailUrl(file: File) {
  return fileThumbnailUrls.get(file) ?? '';
}

function syncFileThumbnailUrls(files: File[]) {
  const imageFiles = new Set(files.filter(isImageFile));
  for (const [file, url] of fileThumbnailUrls) {
    if (imageFiles.has(file)) continue;
    URL.revokeObjectURL(url);
    fileThumbnailUrls.delete(file);
  }
  for (const file of imageFiles) {
    if (!fileThumbnailUrls.has(file)) {
      fileThumbnailUrls.set(file, URL.createObjectURL(file));
    }
  }
}

function revokeFileThumbnailUrls() {
  for (const url of fileThumbnailUrls.values()) {
    URL.revokeObjectURL(url);
  }
  fileThumbnailUrls.clear();
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

function queuePromptCompletionRefresh() {
  void nextTick(refreshPromptCompletion);
}

function refreshPromptCompletion() {
  if (props.disabled || isCollapsed.value) {
    closePromptCompletion();
    return;
  }
  const input = promptInputRef.value?.nativeEl;
  if (!input) return;
  const range = activePromptCompletion(input.value, input.selectionStart ?? input.value.length);
  if (!range || (range.kind === 'file' && !completionScopeReady.value)) {
    closePromptCompletion();
    return;
  }
  completionRange.value = range;
  completionError.value = false;
  activeCompletionIndex.value = 0;
  if (range.kind === 'command') {
    cancelFileSearch();
    void loadSlashCommands();
    return;
  }
  scheduleFileSearch(range.query);
}

async function loadSlashCommands() {
  if (slashCommandsLoaded.value || commandLoading.value) return;
  commandLoading.value = true;
  try {
    slashCommands.value = await listCodexSlashCommands();
    slashCommandsLoaded.value = true;
  } catch {
    completionError.value = true;
  } finally {
    commandLoading.value = false;
  }
}

function scheduleFileSearch(query: string) {
  cancelFileSearch();
  fileMatches.value = [];
  fileLoading.value = true;
  const generation = fileSearchGeneration;
  fileSearchTimer = setTimeout(() => {
    fileSearchTimer = null;
    void runFileSearch(query, generation);
  }, 120);
}

async function runFileSearch(query: string, generation: number) {
  try {
    const matches = await searchPromptFiles({
      query,
      ...(props.completionProjectId ? { projectId: props.completionProjectId } : {}),
      ...(props.completionSessionId ? { sessionId: props.completionSessionId } : {}),
    });
    if (generation !== fileSearchGeneration) return;
    fileMatches.value = matches;
  } catch {
    if (generation === fileSearchGeneration) completionError.value = true;
  } finally {
    if (generation === fileSearchGeneration) fileLoading.value = false;
  }
}

function cancelFileSearch() {
  fileSearchGeneration += 1;
  if (fileSearchTimer) clearTimeout(fileSearchTimer);
  fileSearchTimer = null;
  fileLoading.value = false;
}

function selectCompletion(index: number) {
  const range = completionRange.value;
  const item = completionItems.value[index];
  const input = promptInputRef.value?.nativeEl;
  if (!range || !item || !input) return;
  if (blurTimer) clearTimeout(blurTimer);
  const value = item.kind === 'command' ? item.command.name : formatFileMention(item.file.path);
  const nextPrompt = applyPromptCompletion(input.value, range, value);
  const cursor = range.start + value.length + 1;
  closePromptCompletion();
  if (item.kind === 'file' && !props.mentions.some((mention) => mention.path === item.file.path)) {
    emit('update:mentions', [...props.mentions, { path: item.file.path }]);
  }
  emit('update:prompt', nextPrompt);
  void nextTick(() => {
    const nextInput = promptInputRef.value;
    if (!nextInput) return;
    nextInput.focus();
    nextInput.nativeEl.setSelectionRange(cursor, cursor);
  });
}

function onPromptKeydown(event: KeyboardEvent) {
  if (event.isComposing) return;
  if (completionOpen.value && completionItems.value.length > 0) {
    if (event.key === 'ArrowDown') {
      event.preventDefault();
      activeCompletionIndex.value =
        (activeCompletionIndex.value + 1) % completionItems.value.length;
      return;
    }
    if (event.key === 'ArrowUp') {
      event.preventDefault();
      activeCompletionIndex.value =
        (activeCompletionIndex.value - 1 + completionItems.value.length) %
        completionItems.value.length;
      return;
    }
    if ((event.key === 'Enter' && !event.shiftKey) || event.key === 'Tab') {
      event.preventDefault();
      selectCompletion(activeCompletionIndex.value);
      return;
    }
  }
  if (completionOpen.value && event.key === 'Escape') {
    event.preventDefault();
    closePromptCompletion();
    return;
  }
}

function onPromptKeyup(event: KeyboardEvent) {
  if (['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
    queuePromptCompletionRefresh();
  }
}

function onPromptBlur() {
  blurTimer = setTimeout(closePromptCompletion, 100);
}

function closePromptCompletion() {
  cancelFileSearch();
  completionRange.value = null;
  fileMatches.value = [];
  completionError.value = false;
  activeCompletionIndex.value = 0;
}

function fileMatchSegments(file: PromptFileMatch) {
  return promptMatchSegments(file.path, file.indices);
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

watch(() => props.files, syncFileThumbnailUrls, { immediate: true });
watch(isCollapsed, async (collapsed) => {
  if (collapsed) return;
  await nextTick();
  const input = promptInputRef.value;
  if (!input) return;
  const cursor = input.nativeEl.value.length;
  input.focus();
  input.nativeEl.setSelectionRange(cursor, cursor);
});
watch(() => [props.completionProjectId, props.completionSessionId], closePromptCompletion);
watch(
  () => completionItems.value.length,
  (length) => {
    if (length === 0) activeCompletionIndex.value = 0;
    else activeCompletionIndex.value = Math.min(activeCompletionIndex.value, length - 1);
  },
);

onBeforeUnmount(() => {
  revokePreviewUrl();
  revokeFileThumbnailUrls();
  cancelFileSearch();
  if (blurTimer) clearTimeout(blurTimer);
});
</script>
