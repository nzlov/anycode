<template>
  <section class="artifact-panel">
    <div class="artifact-toolbar">
      <q-input
        v-model="filter"
        dense
        outlined
        clearable
        debounce="300"
        placeholder="筛选产物"
        aria-label="筛选产物"
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
        aria-label="产物类型"
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
        aria-label="产物来源"
      />
      <q-select
        v-model="sort"
        dense
        outlined
        emit-value
        map-options
        :options="sortOptions"
        label="排序"
        aria-label="产物排序"
      />
      <q-btn flat round dense icon="refresh" aria-label="刷新产物" :loading="loading" @click="refresh">
        <q-tooltip>刷新产物</q-tooltip>
      </q-btn>
    </div>

    <q-banner v-if="error" dense class="artifact-error">
      <template #avatar><q-icon name="error_outline" /></template>
      {{ error }}
    </q-banner>

    <q-list bordered separator class="artifact-list">
      <q-item v-if="loading && files.length === 0">
        <q-item-section avatar><q-spinner color="primary" size="24px" /></q-item-section>
        <q-item-section>正在读取产物</q-item-section>
      </q-item>
      <q-item v-else-if="files.length === 0">
        <q-item-section avatar><q-icon name="inventory_2" class="text-muted" /></q-item-section>
        <q-item-section>
          <q-item-label>暂无产物</q-item-label>
        </q-item-section>
      </q-item>
      <q-item
        v-for="file in files"
        :key="file.id"
        clickable
        :active="focusedId === file.id"
        @click="openPreview(file)"
      >
        <q-item-section avatar>
          <q-icon :name="fileIcon(file)" color="primary" />
        </q-item-section>
        <q-item-section>
          <q-item-label class="artifact-name">{{ file.logicalPath || file.filename }}</q-item-label>
          <q-item-label caption
            >{{ formatBytes(file.size) }} · {{ file.artifactKind }}</q-item-label
          >
        </q-item-section>
        <q-item-section side>
          <div class="artifact-actions">
            <q-btn
              flat
              round
              dense
              icon="input"
              aria-label="作为输入使用"
              :loading="copyingId === file.id"
              @click.stop="copyAsInput(file)"
            >
              <q-tooltip>作为输入使用</q-tooltip>
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

    <AppPagination
      v-if="pageMax > 1"
      v-model="page"
      class="justify-center"
      :max="pageMax"
      :disabled="loading"
    />

    <q-dialog v-model="previewOpen" :maximized="$q.screen.lt.sm" @hide="clearPreview">
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
        <q-card-section class="artifact-preview-body">
          <div v-if="previewLoading" class="artifact-preview-state">
            <q-spinner color="primary" size="32px" />
          </div>
          <q-banner v-else-if="previewError" dense class="artifact-error">{{
            previewError
          }}</q-banner>
          <img
            v-else-if="selected?.previewKind === 'image' && previewURL"
            :src="previewURL"
            :alt="selected.filename"
            class="artifact-image"
          />
          <iframe
            v-else-if="selected?.previewKind === 'pdf' && previewURL"
            :src="previewURL"
            class="artifact-frame"
            title="PDF 预览"
          />
          <video
            v-else-if="selected?.previewKind === 'video' && previewURL"
            :src="previewURL"
            class="artifact-media"
            controls
          />
          <audio
            v-else-if="selected?.previewKind === 'audio' && previewURL"
            :src="previewURL"
            class="artifact-audio"
            controls
          />
          <pre v-else-if="selected?.previewKind === 'text'" class="artifact-text">{{
            previewText
          }}</pre>
          <div v-else class="artifact-preview-state text-muted">
            <q-icon name="draft" size="36px" />
            <span>此文件仅支持下载</span>
          </div>
        </q-card-section>
      </q-card>
    </q-dialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Dialog, Notify } from 'quasar';

import AppPagination from '@/components/AppPagination.vue';
import {
  deleteSessionFile,
  downloadSessionFile,
  fetchSessionFile,
  listSessionFiles,
  useSessionFileAsInput,
  type SessionFile,
  type SessionArtifactFocusRequest,
} from '@/services/sessionFiles';

const props = withDefaults(
  defineProps<{
    sessionId: string;
    refreshKey?: string;
    focusRequest?: SessionArtifactFocusRequest | null;
  }>(),
  { refreshKey: '', focusRequest: null },
);
const emit = defineEmits<{
  artifactDeleted: [logicalPath: string];
  artifactsRefreshed: [];
}>();
const files = ref<SessionFile[]>([]);
const loading = ref(false);
const error = ref('');
const filter = ref('');
const kind = ref<string | null>(null);
const source = ref<string | null>(null);
const sort = ref('created_at_desc');
const page = ref(1);
const pageSize = 20;
const total = ref(0);
const deletingId = ref('');
const downloadingId = ref('');
const copyingId = ref('');
const previewOpen = ref(false);
const selected = ref<SessionFile | null>(null);
const previewLoading = ref(false);
const previewError = ref('');
const previewURL = ref('');
const previewText = ref('');
const focusedId = ref('');
let loadRequest = 0;
let previewController: AbortController | null = null;
const pageMax = computed(() => Math.max(1, Math.ceil(total.value / pageSize)));
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
  { label: 'MCP 发布', value: 'mcp_artifact' },
  { label: 'Playwright', value: 'playwright_artifact' },
  { label: '显式发布', value: 'published_artifact' },
  { label: '自动扫描', value: 'reconciled_artifact' },
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
      page: number;
      pageSize: number;
      filter?: string;
      kind?: string;
      source?: string;
      sort?: string;
    } = {
      sessionId: props.sessionId,
      page: page.value,
      pageSize,
    };
    if (filter.value.trim()) input.filter = filter.value.trim();
    if (kind.value) input.kind = kind.value;
    if (source.value) input.source = source.value;
    if (sort.value) input.sort = sort.value;
    const result = await listSessionFiles(input);
    if (request !== loadRequest) return;
    if (result.items.length === 0 && result.pageInfo.total > 0 && page.value > 1) {
      page.value = Math.max(1, Math.ceil(result.pageInfo.total / pageSize));
      return;
    }
    files.value = result.items;
    total.value = result.pageInfo.total;
    page.value = result.pageInfo.page;
  } catch (err) {
    if (request === loadRequest) error.value = errorMessage(err, '读取产物失败');
  } finally {
    if (request === loadRequest) loading.value = false;
  }
}

async function refresh() {
  await load();
  emit('artifactsRefreshed');
}

async function openPreview(file: SessionFile) {
  clearPreviewResource();
  selected.value = file;
  previewOpen.value = true;
  if (file.previewKind === 'none') return;
  if (file.previewKind === 'text' && file.size > 1 << 20) {
    previewError.value = '文本超过 1 MiB，请下载查看';
    return;
  }
  const controller = new AbortController();
  previewController = controller;
  previewLoading.value = true;
  try {
    const blob = await fetchSessionFile(file, 'preview', controller.signal);
    if (previewController !== controller || selected.value?.id !== file.id) return;
    if (file.previewKind === 'text') {
      const content = await blob.text();
      if (previewController === controller && selected.value?.id === file.id) {
        previewText.value = content;
      }
    } else {
      previewURL.value = URL.createObjectURL(blob);
    }
  } catch (err) {
    if (!isAbortError(err) && previewController === controller) {
      previewError.value = errorMessage(err, '预览文件失败');
    }
  } finally {
    if (previewController === controller) {
      previewController = null;
      previewLoading.value = false;
    }
  }
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

async function copyAsInput(file: SessionFile) {
  copyingId.value = file.id;
  try {
    await useSessionFileAsInput(file.id);
    Notify.create({ type: 'positive', message: `已将 ${file.filename} 复制为会话输入` });
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err, '复制输入文件失败') });
  } finally {
    copyingId.value = '';
  }
}

function confirmDelete(file: SessionFile) {
  Dialog.create({
    title: '删除产物',
    message: `确定删除“${file.filename}”吗？`,
    cancel: true,
    persistent: true,
  }).onOk(() => void remove(file));
}

async function remove(file: SessionFile) {
  deletingId.value = file.id;
  try {
    await deleteSessionFile(file.id);
    if (selected.value?.id === file.id) previewOpen.value = false;
    if (focusedId.value === file.id) focusedId.value = '';
    await load();
    emit('artifactDeleted', file.logicalPath);
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err, '删除文件失败') });
  } finally {
    deletingId.value = '';
  }
}

async function applyFocus(request: SessionArtifactFocusRequest) {
  filter.value = request.file.logicalPath;
  kind.value = null;
  source.value = null;
  sort.value = 'created_at_desc';
  page.value = 1;
  focusedId.value = request.file.id;
  await openPreview(request.file);
}

function clearPreviewResource() {
  previewController?.abort();
  previewController = null;
  previewLoading.value = false;
  if (previewURL.value) URL.revokeObjectURL(previewURL.value);
  previewURL.value = '';
  previewText.value = '';
  previewError.value = '';
}

function clearPreview() {
  clearPreviewResource();
  selected.value = null;
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

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

watch(page, () => void load());
watch([filter, kind, source, sort], () => {
  if (page.value !== 1) page.value = 1;
  else void load();
});
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
    if (request) void applyFocus(request);
  },
  { immediate: true },
);
onMounted(() => void load());
onBeforeUnmount(() => {
  loadRequest++;
  clearPreviewResource();
});
</script>

<style scoped>
.artifact-panel,
.artifact-preview-body {
  display: grid;
  min-width: 0;
  gap: 12px;
}

.artifact-toolbar,
.artifact-preview-header,
.artifact-preview-title,
.artifact-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.artifact-toolbar {
  min-width: 0;
  flex-wrap: wrap;
}

.artifact-toolbar .q-input {
  min-width: 0;
  flex: 1 1 calc(100% - 40px);
}

.artifact-toolbar .q-select {
  width: calc(33.333% - 6px);
  min-width: 0;
  flex: 1 1 calc(33.333% - 6px);
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

.artifact-error {
  color: var(--q-negative);
  background: var(--ac-surface-muted);
}

.artifact-preview-dialog {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.artifact-preview-body {
  min-height: 260px;
  flex: 1 1 auto;
  place-items: center;
  overflow: auto;
  background: var(--ac-surface-muted);
}

.artifact-preview-state {
  display: grid;
  place-items: center;
  gap: 8px;
}

.artifact-image,
.artifact-media {
  display: block;
  max-width: 100%;
  max-height: 72vh;
  object-fit: contain;
}

.artifact-frame {
  width: 100%;
  min-height: 68vh;
  border: 0;
}

.artifact-audio {
  width: min(100%, 520px);
}

.artifact-text {
  width: 100%;
  margin: 0;
  overflow: auto;
  color: var(--ac-text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
