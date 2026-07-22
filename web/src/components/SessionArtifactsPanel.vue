<template>
  <section
    ref="panelElement"
    class="artifact-panel"
    :class="{ 'artifact-panel--inline-enabled': inlinePreview }"
  >
    <div class="artifact-toolbar">
      <q-input
        v-model="filter"
        dense
        outlined
        clearable
        debounce="300"
        placeholder="筛选临时文件"
        aria-label="筛选临时文件"
      >
        <template #prepend><q-icon name="search" /></template>
      </q-input>
      <q-select
        v-model="kind"
        dense
        outlined
        clearable
        emit-value
        map-options
        :options="kindOptions"
        label="类型"
        aria-label="临时文件类型"
      />
      <q-select
        v-model="source"
        dense
        outlined
        clearable
        emit-value
        map-options
        :options="sourceOptions"
        label="来源"
        aria-label="临时文件来源"
      />
      <q-select
        v-model="sort"
        dense
        outlined
        emit-value
        map-options
        :options="sortOptions"
        label="排序"
        aria-label="临时文件排序"
      />
      <q-btn
        flat
        round
        dense
        icon="refresh"
        aria-label="刷新临时文件"
        :loading="loading"
        @click="refresh"
      >
        <q-tooltip>刷新临时文件</q-tooltip>
      </q-btn>
    </div>

    <q-banner v-if="error" dense class="artifact-error">
      <template #avatar><q-icon name="error_outline" /></template>
      {{ error }}
    </q-banner>

    <div class="artifact-layout">
      <q-list bordered separator class="artifact-list">
        <q-item v-if="loading && files.length === 0">
          <q-item-section avatar><q-spinner color="primary" size="24px" /></q-item-section>
          <q-item-section>正在读取临时文件</q-item-section>
        </q-item>
        <q-item v-else-if="files.length === 0">
          <q-item-section avatar><q-icon name="inventory_2" class="text-muted" /></q-item-section>
          <q-item-section>
            <q-item-label>暂无临时文件</q-item-label>
          </q-item-section>
        </q-item>
        <q-item
          v-for="file in files"
          :key="file.id"
          clickable
          class="artifact-list-item"
          :active="focusedId === file.id || (inlinePreviewActive && selected?.id === file.id)"
          @click="openPreview(file)"
        >
          <q-item-section avatar class="artifact-list-item__avatar">
            <q-icon :name="fileIcon(file)" color="primary" />
          </q-item-section>
          <q-item-section class="artifact-list-item__content">
            <q-item-label class="artifact-name">{{
              file.logicalPath || file.filename
            }}</q-item-label>
            <q-item-label caption
              >{{ formatBytes(file.size) }} · {{ file.artifactKind }}</q-item-label
            >
          </q-item-section>
          <q-item-section side class="artifact-list-item__side">
            <div class="artifact-actions">
              <q-btn
                v-if="allowReference"
                flat
                round
                dense
                icon="add_link"
                aria-label="引用到当前提示"
                @click.stop="emit('referenceArtifact', file)"
              >
                <q-tooltip>引用到当前提示</q-tooltip>
              </q-btn>
              <q-btn
                flat
                round
                dense
                icon="download"
                aria-label="下载文件"
                :loading="downloadingId === file.id"
                @click.stop="download(file)"
              >
                <q-tooltip>下载</q-tooltip>
              </q-btn>
              <q-btn
                flat
                round
                dense
                color="negative"
                icon="delete_outline"
                aria-label="删除文件"
                :loading="deletingId === file.id"
                @click.stop="confirmDelete(file)"
              >
                <q-tooltip>删除</q-tooltip>
              </q-btn>
            </div>
          </q-item-section>
        </q-item>
      </q-list>

      <q-card v-if="inlinePreviewActive" flat bordered class="artifact-inline-preview">
        <q-card-section class="artifact-preview-header">
          <div class="artifact-preview-title">
            <q-icon v-if="selected" :name="fileIcon(selected)" />
            <span>{{ selected?.logicalPath || selected?.filename || '临时文件' }}</span>
          </div>
          <q-btn
            v-if="selected"
            flat
            round
            dense
            icon="download"
            aria-label="下载文件"
            :loading="downloadingId === selected.id"
            @click="download(selected)"
          >
            <q-tooltip>下载</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />
        <SessionFilePreview :file="selected" />
      </q-card>
    </div>

    <q-dialog v-model="previewOpen" :maximized="$q.screen.lt.sm" @hide="handlePreviewDialogHide">
      <q-card class="artifact-preview-dialog app-content-dialog">
        <q-card-section class="artifact-preview-header">
          <div class="artifact-preview-title">
            <q-icon v-if="selected" :name="fileIcon(selected)" />
            <span>{{ selected?.logicalPath || selected?.filename || '文件预览' }}</span>
          </div>
          <div class="artifact-actions">
            <q-btn
              v-if="selected"
              flat
              round
              dense
              icon="download"
              aria-label="下载文件"
              @click="download(selected)"
            >
              <q-tooltip>下载</q-tooltip>
            </q-btn>
            <q-btn v-close-popup flat round dense icon="close" aria-label="关闭">
              <q-tooltip>关闭</q-tooltip>
            </q-btn>
          </div>
        </q-card-section>
        <q-separator />
        <SessionFilePreview :file="selected" />
      </q-card>
    </q-dialog>
  </section>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Dialog, Notify } from 'quasar';

import SessionFilePreview from '@/components/SessionFilePreview.vue';
import {
  deleteSessionFile,
  downloadSessionFile,
  listSessionFiles,
  type SessionFile,
  type SessionArtifactFocusRequest,
} from '@/services/sessionFiles';

const props = withDefaults(
  defineProps<{
    sessionId: string;
    refreshKey?: string;
    focusRequest?: SessionArtifactFocusRequest | null;
    inlinePreview?: boolean;
    allowReference?: boolean;
  }>(),
  { refreshKey: '', focusRequest: null, inlinePreview: false, allowReference: false },
);
const emit = defineEmits<{
  artifactDeleted: [file: SessionFile];
  artifactsRefreshed: [];
  referenceArtifact: [file: SessionFile];
}>();
const files = ref<SessionFile[]>([]);
const panelElement = ref<HTMLElement | null>(null);
const loading = ref(false);
const error = ref('');
const filter = ref('');
const kind = ref<string | null>(null);
const source = ref<string | null>(null);
const sort = ref('created_at_desc');
const deletingId = ref('');
const downloadingId = ref('');
const previewOpen = ref(false);
const selected = ref<SessionFile | null>(null);
const focusedId = ref('');
const inlinePreviewActive = ref(false);
let loadRequest = 0;
let panelResizeObserver: ResizeObserver | null = null;
const inlinePreviewMinWidth = 1024;
const kindOptions = [
  { label: '图片', value: 'image' },
  { label: 'PDF', value: 'pdf' },
  { label: '视频', value: 'video' },
  { label: '音频', value: 'audio' },
  { label: '压缩包', value: 'archive' },
  { label: '文本', value: 'text' },
  { label: '其他文件', value: 'file' },
];
const sourceOptions = [
  { label: 'Codex', value: 'codex_artifact' },
  { label: 'Playwright', value: 'playwright_artifact' },
];
const sortOptions = [
  { label: '最新优先', value: 'created_at_desc' },
  { label: '最早优先', value: 'created_at_asc' },
  { label: '文件名', value: 'filename_asc' },
  { label: '大小', value: 'size_desc' },
];

async function load() {
  if (!props.sessionId) return;
  const request = ++loadRequest;
  loading.value = true;
  error.value = '';
  try {
    const input: {
      sessionId: string;
      filter?: string;
      kind?: string;
      source?: string;
      sort?: string;
    } = {
      sessionId: props.sessionId,
    };
    if (filter.value.trim()) input.filter = filter.value.trim();
    if (kind.value) input.kind = kind.value;
    if (source.value) input.source = source.value;
    if (sort.value) input.sort = sort.value;
    const result = await listSessionFiles(input);
    if (request !== loadRequest) return;
    files.value = result;
    if (inlinePreviewActive.value) syncInlineSelection(result);
  } catch (err) {
    if (request === loadRequest) error.value = errorMessage(err, '读取临时文件失败');
  } finally {
    if (request === loadRequest) loading.value = false;
  }
}

async function refresh() {
  await load();
  emit('artifactsRefreshed');
}

function openPreview(file: SessionFile) {
  selected.value = file;
  if (!inlinePreviewActive.value) previewOpen.value = true;
}

function syncInlineSelection(nextFiles: SessionFile[]) {
  if (!inlinePreviewActive.value) return;
  if (nextFiles.length === 0) {
    clearPreview();
    return;
  }
  const current = selected.value
    ? nextFiles.find((file) => file.id === selected.value?.id)
    : undefined;
  if (current) {
    selected.value = current;
    return;
  }
  const first = nextFiles[0];
  if (first) selected.value = first;
}

async function download(file: SessionFile) {
  downloadingId.value = file.id;
  try {
    await downloadSessionFile(file);
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err, '下载文件失败') });
  } finally {
    downloadingId.value = '';
  }
}

function confirmDelete(file: SessionFile) {
  Dialog.create({
    title: '删除临时文件',
    message: `确定删除“${file.filename}”吗？`,
    cancel: true,
    persistent: true,
  }).onOk(() => void remove(file));
}

async function remove(file: SessionFile) {
  deletingId.value = file.id;
  try {
    await deleteSessionFile(file.id);
    if (selected.value?.id === file.id) {
      previewOpen.value = false;
      clearPreview();
    }
    if (focusedId.value === file.id) focusedId.value = '';
    await load();
    emit('artifactDeleted', file);
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err, '删除文件失败') });
  } finally {
    deletingId.value = '';
  }
}

function applyFocus(request: SessionArtifactFocusRequest) {
  filter.value = request.file.logicalPath;
  kind.value = null;
  source.value = null;
  sort.value = 'created_at_desc';
  focusedId.value = request.file.id;
  openPreview(request.file);
}

function clearPreview() {
  selected.value = null;
}

function handlePreviewDialogHide() {
  if (!inlinePreviewActive.value) clearPreview();
}

function updateInlinePreview(width: number) {
  const active = props.inlinePreview && width >= inlinePreviewMinWidth;
  if (active === inlinePreviewActive.value) return;
  inlinePreviewActive.value = active;
  if (active) {
    previewOpen.value = false;
    syncInlineSelection(files.value);
  } else if (!previewOpen.value) {
    clearPreview();
  }
}

function observePanelWidth() {
  const element = panelElement.value;
  if (!props.inlinePreview || !element || typeof ResizeObserver === 'undefined') return;
  updateInlinePreview(element.getBoundingClientRect().width);
  panelResizeObserver = new ResizeObserver((entries) => {
    const entry = entries[0];
    if (entry) updateInlinePreview(entry.contentRect.width);
  });
  panelResizeObserver.observe(element);
}

function fileIcon(file: SessionFile) {
  const icons: Record<string, string> = {
    image: 'image',
    pdf: 'picture_as_pdf',
    video: 'movie',
    audio: 'audio_file',
    archive: 'folder_zip',
    text: 'description',
  };
  return icons[file.artifactKind] ?? 'draft';
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1 << 20) return `${(value / 1024).toFixed(1)} KiB`;
  if (value < 1 << 30) return `${(value / (1 << 20)).toFixed(1)} MiB`;
  return `${(value / (1 << 30)).toFixed(1)} GiB`;
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback;
}

watch([filter, kind, source, sort], () => void load());
watch(
  () => props.refreshKey,
  (next, previous) => {
    if (next && next !== previous) void load();
  },
);
watch(
  () => props.focusRequest?.token,
  () => {
    const request = props.focusRequest;
    if (request) applyFocus(request);
  },
  { immediate: true },
);
onMounted(() => {
  observePanelWidth();
  void load();
});
onBeforeUnmount(() => {
  loadRequest++;
  panelResizeObserver?.disconnect();
  panelResizeObserver = null;
});
</script>

<style scoped>
.artifact-panel {
  display: grid;
  min-width: 0;
  gap: 12px;
}

.artifact-panel {
  container-type: inline-size;
}

.artifact-panel--inline-enabled {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
}

.artifact-layout {
  min-width: 0;
}

.artifact-panel--inline-enabled .artifact-layout {
  min-height: 0;
  flex: 1 1 auto;
}

.artifact-preview-header,
.artifact-preview-title,
.artifact-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.artifact-toolbar {
  display: grid;
  min-width: 0;
  grid-template-columns: minmax(0, 1fr) repeat(3, minmax(112px, 160px)) 40px;
  align-items: center;
  gap: 8px;
}

.artifact-toolbar .q-input {
  min-width: 0;
}

.artifact-toolbar .q-select {
  width: auto;
  min-width: 0;
}

.artifact-preview-header {
  justify-content: space-between;
}

.artifact-preview-title {
  min-width: 0;
  font-weight: 600;
}

.artifact-preview-title span,
.artifact-name {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.artifact-list,
.artifact-error {
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.artifact-list {
  color: var(--ac-text);
  background: var(--ac-surface);
}

.artifact-list-item.q-item--active {
  color: var(--ac-on-secondary-container);
  background: var(--ac-secondary-container);
}

.artifact-error {
  color: var(--ac-on-error-container);
  background: var(--ac-error-container);
}

.artifact-preview-dialog {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.artifact-inline-preview {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  color: var(--ac-text);
  background: var(--ac-surface);
  border-color: var(--ac-border);
}

@container (min-width: 1024px) {
  .artifact-panel--inline-enabled .artifact-layout {
    display: grid;
    min-height: 0;
    grid-template-columns: minmax(320px, 36%) minmax(0, 1fr);
    grid-template-rows: minmax(0, 1fr);
    gap: 16px;
    overflow: hidden;
  }

  .artifact-panel--inline-enabled .artifact-list {
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
  }

  .artifact-inline-preview :deep(.session-file-preview) {
    min-height: 0;
  }

  .artifact-inline-preview :deep(.session-file-preview__image),
  .artifact-inline-preview :deep(.session-file-preview__media) {
    max-height: 100%;
  }
}

@container (max-width: 639px) {
  .artifact-toolbar {
    grid-template-columns: repeat(2, minmax(0, 1fr)) 40px;
  }

  .artifact-toolbar .q-input {
    grid-row: 1;
    grid-column: 1 / 3;
  }

  .artifact-toolbar > .q-btn {
    grid-row: 1;
    grid-column: 3;
  }

  .artifact-toolbar .q-select:last-of-type {
    grid-column: 1 / 3;
  }

  .artifact-list-item {
    display: grid !important;
    grid-template-columns: 40px minmax(0, 1fr);
    align-items: flex-start;
  }

  .artifact-list-item__avatar {
    grid-row: 1;
    grid-column: 1;
    min-width: 40px;
  }

  .artifact-list-item__content {
    grid-row: 1;
    grid-column: 2;
    min-width: 0;
  }

  .artifact-list-item__side {
    width: auto;
    max-width: 100%;
    box-sizing: border-box;
    grid-row: 2;
    grid-column: 2;
    align-items: flex-end;
    padding-top: 4px;
    padding-left: 0;
  }
}
</style>
