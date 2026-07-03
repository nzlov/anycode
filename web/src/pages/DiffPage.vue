<template>
  <q-page class="page-shell diff-page">
    <div class="page-heading">
      <div class="heading-copy">
        <div class="text-h5 text-weight-bold">当前分支变更</div>
        <div class="text-body2 text-muted">
          <template v-if="sessionId">会话 {{ sessionId }}</template>
          <template v-else>缺少 sessionId，无法读取 Diff</template>
        </div>
      </div>
      <div class="heading-actions">
        <q-select
          v-model="pageSize"
          dense
          outlined
          emit-value
          map-options
          class="page-size-select"
          :options="pageSizeOptions"
          :disable="loading"
          label="每页文件"
        />
        <q-btn-toggle
          v-model="viewMode"
          no-caps
          unelevated
          toggle-color="primary"
          :disable="loading"
          :options="[
            { label: '单个文件', value: 'single', icon: 'description' },
            { label: '全部 Diff', value: 'all', icon: 'difference' },
          ]"
        />
      </div>
    </div>

    <q-banner v-if="!sessionId" rounded class="state-banner bg-warning text-dark">
      请从会话详情进入 Diff 页面，或在地址 query 中提供 sessionId。
    </q-banner>

    <q-banner v-else-if="error" rounded class="state-banner bg-negative text-white">
      <template #avatar>
        <q-icon name="error" />
      </template>
      {{ error }}
      <template #action>
        <q-btn flat color="white" label="重试" no-caps :loading="loading" @click="loadDiff" />
      </template>
    </q-banner>

    <q-banner
      v-else-if="diff && !diff.available"
      rounded
      class="state-banner bg-grey-2 text-grey-8"
    >
      <template #avatar>
        <q-icon name="block" />
      </template>
      当前会话没有可用 worktree Diff，可能是非 git 项目或会话尚未创建 worktree。
    </q-banner>

    <div v-if="sessionId && !error && (!diff || diff.available)" class="diff-layout">
      <q-card flat bordered class="diff-files">
        <q-inner-loading :showing="loading">
          <q-spinner color="primary" size="32px" />
        </q-inner-loading>

        <q-card-section class="files-header">
          <div>
            <div class="text-subtitle2 text-weight-bold">文件</div>
            <div class="text-caption text-muted">{{ fileCountLabel }}</div>
          </div>
          <q-btn flat round dense icon="refresh" :loading="loading" @click="loadDiff">
            <q-tooltip>刷新 Diff</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />

        <q-card-section v-if="!loading && diff?.files.length === 0" class="empty-state">
          <q-icon name="task_alt" size="32px" color="positive" />
          <div class="text-body2">暂无文件变更</div>
        </q-card-section>

        <q-list v-else separator>
          <q-item
            v-for="file in diff?.files"
            :key="file.path"
            clickable
            :active="selectedPath === file.path && viewMode === 'single'"
            active-class="active-file"
            @click="selectFile(file.path)"
          >
            <q-item-section avatar>
              <q-icon :name="fileIcon(file.status)" :color="fileColor(file.status)" />
            </q-item-section>
            <q-item-section>
              <q-item-label class="file-path">{{ file.path }}</q-item-label>
              <q-item-label caption>
                <span class="text-positive">+{{ file.additions }}</span>
                <span class="q-mx-xs">/</span>
                <span class="text-negative">-{{ file.deletions }}</span>
              </q-item-label>
            </q-item-section>
          </q-item>
        </q-list>

        <q-separator v-if="showPagination" />
        <q-card-actions v-if="showPagination" align="center" class="files-pagination">
          <q-pagination
            v-model="page"
            dense
            boundary-numbers
            direction-links
            :max="pageMax"
            :max-pages="5"
            :disable="loading"
          />
        </q-card-actions>
      </q-card>

      <section class="diff-content">
        <q-card v-if="loading && !diff" flat bordered class="diff-state-card">
          <q-card-section class="empty-state">
            <q-spinner color="primary" size="32px" />
            <div class="text-body2 text-muted">正在读取 Diff</div>
          </q-card-section>
        </q-card>

        <q-card v-else-if="visibleDiffs.length === 0" flat bordered class="diff-state-card">
          <q-card-section class="empty-state">
            <q-icon name="data_object" size="32px" color="grey-6" />
            <div class="text-body2">当前范围没有可展示的 Diff</div>
          </q-card-section>
        </q-card>

        <q-card
          v-for="fileDiff in visibleDiffs"
          :key="fileDiff.file.path"
          flat
          bordered
          class="diff-file-card"
        >
          <q-card-section class="diff-file-header">
            <div class="file-title">
              <q-icon
                :name="fileIcon(fileDiff.file.status)"
                :color="fileColor(fileDiff.file.status)"
              />
              <span>{{ fileDiff.file.path }}</span>
            </div>
            <div class="row items-center q-gutter-sm">
              <q-badge outline color="positive" :label="`+${fileDiff.file.additions}`" />
              <q-badge outline color="negative" :label="`-${fileDiff.file.deletions}`" />
              <q-badge
                outline
                :color="fileColor(fileDiff.file.status)"
                :label="fileDiff.file.status"
              />
            </div>
          </q-card-section>
          <q-separator />
          <q-card-section class="diff-code">
            <div
              v-for="line in fileDiff.lines"
              :key="`${fileDiff.file.path}:${line.id}`"
              class="diff-line"
              :class="lineClass(line.kind)"
            >
              <span class="line-number">{{ line.oldLine ?? '' }}</span>
              <span class="line-number">{{ line.newLine ?? '' }}</span>
              <pre>{{ line.content }}</pre>
            </div>
          </q-card-section>
        </q-card>
      </section>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { getSessionDiff } from '@/services/diff';
import type { DiffFile, DiffLineKind, DiffMode, FileDiff, SessionDiff } from '@/services/diff';

const route = useRoute();
const router = useRouter();

const pageSizeOptions = [
  { label: '10', value: 10 },
  { label: '20', value: 20 },
  { label: '50', value: 50 },
];

const sessionId = computed(() => stringQuery(route.query.sessionId));
const viewMode = ref<DiffMode>(normalizeMode(stringQuery(route.query.mode)));
const selectedPath = ref(stringQuery(route.query.filePath));
const page = ref(positiveIntQuery(route.query.page, 1));
const pageSize = ref(positiveIntQuery(route.query.pageSize, 20));
const diff = ref<SessionDiff | null>(null);
const loading = ref(false);
const error = ref('');

const visibleDiffs = computed<FileDiff[]>(() => {
  if (!diff.value?.available) {
    return [];
  }
  return viewMode.value === 'all'
    ? diff.value.allDiff
    : diff.value.fileDiff
      ? [diff.value.fileDiff]
      : [];
});
const pageMax = computed(() => {
  const info = diff.value?.pageInfo;
  if (!info || info.total < 1) return 1;
  return Math.max(1, Math.ceil(info.total / info.pageSize));
});
const showPagination = computed(() => pageMax.value > 1);
const fileCountLabel = computed(() => {
  const info = diff.value?.pageInfo;
  if (!info) return '等待加载';
  return `第 ${info.page} 页，共 ${info.total} 个文件`;
});

async function loadDiff() {
  if (!sessionId.value) return;
  loading.value = true;
  error.value = '';
  try {
    const input: Parameters<typeof getSessionDiff>[0] = {
      sessionId: sessionId.value,
      mode: viewMode.value,
      page: page.value,
      pageSize: pageSize.value,
    };
    if (viewMode.value === 'single' && selectedPath.value) {
      input.filePath = selectedPath.value;
    }
    const nextDiff = await getSessionDiff(input);
    diff.value = nextDiff;
    page.value = nextDiff.pageInfo.page;
    pageSize.value = nextDiff.pageInfo.pageSize;
    if (viewMode.value === 'single') {
      selectedPath.value =
        nextDiff.filePath || nextDiff.fileDiff?.file.path || nextDiff.files[0]?.path || '';
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取 Diff 失败';
  } finally {
    loading.value = false;
  }
}

function selectFile(path: string) {
  viewMode.value = 'single';
  selectedPath.value = path;
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

function stringQuery(value: unknown) {
  if (Array.isArray(value)) return String(value[0] ?? '');
  return typeof value === 'string' ? value : '';
}

function positiveIntQuery(value: unknown, fallback: number) {
  const parsed = Number.parseInt(stringQuery(value), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function normalizeMode(mode: string): DiffMode {
  return mode === 'all' ? 'all' : 'single';
}

function syncRouteQuery() {
  if (!sessionId.value) return;
  const nextQuery = {
    ...route.query,
    sessionId: sessionId.value,
    mode: viewMode.value,
    filePath: viewMode.value === 'single' && selectedPath.value ? selectedPath.value : undefined,
    page: String(page.value),
    pageSize: String(pageSize.value),
  };
  const current = JSON.stringify(route.query);
  const next = JSON.stringify(nextQuery);
  if (current !== next) {
    void router.replace({ query: nextQuery });
  }
}

watch([viewMode, selectedPath, page, pageSize], () => {
  syncRouteQuery();
  void loadDiff();
});

watch(
  () => route.query.sessionId,
  () => {
    void loadDiff();
  },
);

onMounted(() => {
  void loadDiff();
});
</script>

<style scoped>
.diff-page {
  min-width: 0;
}

.heading-copy {
  min-width: 0;
}

.heading-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: flex-end;
}

.page-size-select {
  width: 132px;
}

.state-banner {
  margin-bottom: 16px;
}

.files-header,
.files-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.files-header {
  gap: 8px;
  padding: 12px 16px;
}

.file-path {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.active-file {
  color: var(--q-primary);
  background: color-mix(in srgb, var(--q-primary) 10%, var(--ac-surface));
}

.empty-state {
  display: grid;
  min-height: 160px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--ac-text-muted);
  text-align: center;
}

.diff-state-card {
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
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
  color: var(--ac-text-muted);
  text-align: right;
  user-select: none;
}

.line-add {
  background: color-mix(in srgb, var(--q-positive) 15%, transparent);
}

.line-delete {
  background: color-mix(in srgb, var(--q-negative) 14%, transparent);
}

.line-header {
  background: color-mix(in srgb, var(--q-primary) 9%, var(--ac-surface-muted));
  color: var(--ac-text-muted);
}

.line-context {
  background: var(--ac-surface);
}

@media (max-width: 699px) {
  .heading-actions,
  .page-size-select {
    width: 100%;
  }

  .diff-line {
    grid-template-columns: 44px 44px minmax(max-content, 1fr);
  }
}
</style>
