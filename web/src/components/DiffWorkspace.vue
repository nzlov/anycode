<template>
  <div class="diff-workspace">
    <div class="diff-workspace__toolbar">
      <q-select
        :model-value="modelValue.pageSize"
        dense
        outlined
        emit-value
        map-options
        class="diff-workspace__page-size"
        :options="pageSizeOptions"
        :disable="loading"
        label="每页文件"
        @update:model-value="setPageSize"
      />
      <q-btn-toggle
        :model-value="modelValue.mode"
        no-caps
        unelevated
        toggle-color="dark"
        :disable="loading"
        :options="[
          { label: '单个文件', value: 'single', icon: 'description' },
          { label: '全部 Diff', value: 'all', icon: 'difference' },
        ]"
        @update:model-value="setMode"
      />
      <q-space />
      <template v-if="modelValue.mode === 'all'">
        <q-btn
          flat
          round
          dense
          icon="unfold_more"
          aria-label="展开当前页全部文件"
          :disable="loading || currentPagePaths.length === 0 || !hasCollapsedCurrentPageFile"
          @click="expandCurrentPage"
        >
          <q-tooltip>展开当前页全部文件</q-tooltip>
        </q-btn>
        <q-btn
          flat
          round
          dense
          icon="unfold_less"
          aria-label="折叠当前页全部文件"
          :disable="loading || currentPagePaths.length === 0 || allCurrentPageFilesCollapsed"
          @click="collapseCurrentPage"
        >
          <q-tooltip>折叠当前页全部文件</q-tooltip>
        </q-btn>
      </template>
      <q-btn
        flat
        round
        dense
        icon="refresh"
        aria-label="刷新 Diff"
        :loading="loading"
        @click="loadDiff"
      >
        <q-tooltip>刷新 Diff</q-tooltip>
      </q-btn>
    </div>

    <q-banner v-if="diff && !diff.available" rounded class="state-banner bg-grey-2 text-grey-8">
      <template #avatar>
        <q-icon name="block" />
      </template>
      当前范围没有可用 Diff，可能是非 git 项目、项目当前未检出该分支，或没有工作区变更。
    </q-banner>

    <div v-if="!diff || diff.available" class="diff-workspace__layout">
      <q-card flat bordered class="diff-files">
        <q-inner-loading :showing="loading">
          <q-spinner color="primary" size="32px" />
        </q-inner-loading>

        <q-card-section class="diff-workspace__files-header">
          <div>
            <div class="text-subtitle2 text-weight-bold">文件</div>
            <div class="text-caption text-muted">{{ fileCountLabel }}</div>
          </div>
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
            :active="modelValue.filePath === file.path && modelValue.mode === 'single'"
            active-class="active-file"
            @click="selectFile(file.path)"
          >
            <q-item-section avatar>
              <q-icon :name="fileIcon(file.status)" :color="fileColor(file.status)" />
            </q-item-section>
            <q-item-section>
              <q-item-label class="file-path">
                <q-btn
                  v-if="sessionPrefix(file.path)"
                  flat
                  dense
                  no-caps
                  class="session-prefix-link"
                  :label="sessionPrefix(file.path)"
                  @click.stop="openPrefixedSession(file.path)"
                  @keydown.stop
                >
                  <q-tooltip>打开会话详情</q-tooltip>
                </q-btn>
                <span>{{ filePathWithoutPrefix(file.path) }}</span>
              </q-item-label>
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
          <AppPagination
            :model-value="modelValue.page"
            :max="pageMax"
            :disabled="loading"
            @update:model-value="setPage"
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

        <DiffViewer
          :file-diffs="visibleDiffs"
          :collapsible="modelValue.mode === 'all'"
          :collapsed-paths="collapseState.collapsedPaths"
          @expand="expandDiff"
          @toggle-collapse="toggleFileCollapsed"
        >
          <template #file-title="{ file }">
            <template v-if="file">
              <q-btn
                v-if="sessionPrefix(file.path)"
                flat
                dense
                no-caps
                class="session-prefix-link"
                :label="sessionPrefix(file.path)"
                @click.stop="openPrefixedSession(file.path)"
                @keydown.stop
              >
                <q-tooltip>打开会话详情</q-tooltip>
              </q-btn>
              <span>{{ filePathWithoutPrefix(file.path) }}</span>
            </template>
          </template>
        </DiffViewer>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { Notify } from 'quasar';
import { useRouter } from 'vue-router';

import AppPagination from '@/components/AppPagination.vue';
import DiffViewer from '@/components/DiffViewer.vue';
import {
  getBranchAllDiff,
  getBranchSingleDiff,
  getSessionAllDiff,
  getSessionSingleDiff,
} from '@/services/diff';
import {
  collapseCurrentDiffPage,
  expandCurrentDiffPage,
  expandDiffContext,
  initialDiffCollapseState,
  initialDiffContext,
  isDiffFileCollapsed,
  syncDiffCollapseTarget,
  toggleDiffFileCollapsed,
} from '@/services/diffViewerState';
import type {
  DiffFile,
  DiffMode,
  DiffWorkspaceState,
  DiffWorkspaceTarget,
  FileDiff,
  SessionDiff,
} from '@/services/diff';
import { listSessions } from '@/services/sessions';

const props = defineProps<{
  target: DiffWorkspaceTarget;
  modelValue: DiffWorkspaceState;
}>();

const emit = defineEmits<{
  'update:modelValue': [state: DiffWorkspaceState];
}>();

const router = useRouter();
const pageSizeOptions = [
  { label: '10', value: 10 },
  { label: '20', value: 20 },
  { label: '50', value: 50 },
];
const diff = ref<SessionDiff | null>(null);
const loading = ref(false);
const sessionPrefixMap = ref<Record<string, string>>({});
const diffContext = ref(initialDiffContext());
const requestGeneration = ref(0);
let skipRequestSignature = '';

const targetKey = computed(() =>
  props.target.kind === 'session'
    ? `session:${props.target.sessionId}`
    : `branch:${props.target.projectId}:${props.target.branch}`,
);
const collapseState = ref(initialDiffCollapseState(targetKey.value));
const visibleDiffs = computed<FileDiff[]>(() => {
  if (!diff.value?.available) return [];
  return props.modelValue.mode === 'all'
    ? diff.value.allDiff
    : diff.value.fileDiff
      ? [diff.value.fileDiff]
      : [];
});
const currentPagePaths = computed(() => diff.value?.allDiff.map((item) => item.file.path) ?? []);
const hasCollapsedCurrentPageFile = computed(() =>
  currentPagePaths.value.some((path) =>
    isDiffFileCollapsed(collapseState.value, props.modelValue.mode, path),
  ),
);
const allCurrentPageFilesCollapsed = computed(
  () =>
    currentPagePaths.value.length > 0 &&
    currentPagePaths.value.every((path) =>
      isDiffFileCollapsed(collapseState.value, props.modelValue.mode, path),
    ),
);
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

function updateState(patch: Partial<DiffWorkspaceState>) {
  const next = { ...props.modelValue, ...patch };
  if (
    next.mode === props.modelValue.mode &&
    next.filePath === props.modelValue.filePath &&
    next.page === props.modelValue.page &&
    next.pageSize === props.modelValue.pageSize
  ) {
    return;
  }
  emit('update:modelValue', next);
}

function setMode(value: DiffMode) {
  updateState({ mode: value, page: 1 });
}

function setPage(value: number) {
  updateState({ page: value });
}

function setPageSize(value: number) {
  updateState({ page: 1, pageSize: value });
}

function selectFile(path: string) {
  updateState({ mode: 'single', filePath: path });
}

function requestSignature(state = props.modelValue) {
  return [targetKey.value, state.mode, state.filePath, state.page, state.pageSize].join('|');
}

async function loadDiff() {
  const generation = ++requestGeneration.value;
  loading.value = true;
  try {
    const input = {
      mode: props.modelValue.mode,
      page: props.modelValue.page,
      pageSize: props.modelValue.pageSize,
      contextBefore: diffContext.value.before,
      contextAfter: diffContext.value.after,
      ...(props.modelValue.mode === 'single' && props.modelValue.filePath
        ? { filePath: props.modelValue.filePath }
        : {}),
    };
    const nextDiff = await requestDiff(input);
    if (generation !== requestGeneration.value) return;
    diff.value = nextDiff;
    const normalizedState: DiffWorkspaceState = {
      ...props.modelValue,
      page: nextDiff.pageInfo.page,
      pageSize: nextDiff.pageInfo.pageSize,
      filePath:
        props.modelValue.mode === 'single'
          ? nextDiff.filePath || nextDiff.fileDiff?.file.path || nextDiff.files[0]?.path || ''
          : props.modelValue.filePath,
    };
    if (requestSignature(normalizedState) !== requestSignature()) {
      skipRequestSignature = requestSignature(normalizedState);
      emit('update:modelValue', normalizedState);
    }
    if (props.target.kind === 'branch') {
      await loadSessionPrefixMap(generation);
    }
  } catch (err) {
    if (generation === requestGeneration.value) notifyError(err, '读取 Diff 失败');
  } finally {
    if (generation === requestGeneration.value) loading.value = false;
  }
}

function requestDiff(input: {
  mode: DiffMode;
  filePath?: string;
  page: number;
  pageSize: number;
  contextBefore: number;
  contextAfter: number;
}) {
  if (props.target.kind === 'branch') {
    const branchInput = {
      ...input,
      projectId: props.target.projectId,
      branch: props.target.branch,
    };
    return input.mode === 'all' ? getBranchAllDiff(branchInput) : getBranchSingleDiff(branchInput);
  }
  const sessionInput = { ...input, sessionId: props.target.sessionId };
  return input.mode === 'all'
    ? getSessionAllDiff(sessionInput)
    : getSessionSingleDiff(sessionInput);
}

function expandDiff(_filePath: string, direction: 'before' | 'after') {
  diffContext.value = expandDiffContext(diffContext.value, direction);
  void loadDiff();
}

function toggleFileCollapsed(filePath: string) {
  collapseState.value = toggleDiffFileCollapsed(
    collapseState.value,
    props.modelValue.mode,
    filePath,
  );
}

function expandCurrentPage() {
  collapseState.value = expandCurrentDiffPage(
    collapseState.value,
    props.modelValue.mode,
    currentPagePaths.value,
  );
}

function collapseCurrentPage() {
  collapseState.value = collapseCurrentDiffPage(
    collapseState.value,
    props.modelValue.mode,
    currentPagePaths.value,
  );
}

async function loadSessionPrefixMap(generation: number) {
  if (props.target.kind !== 'branch') return;
  const target = props.target;
  const prefixTargetKey = targetKey.value;
  const map: Record<string, string> = {};
  let loaded = 0;
  // GLUE: branch Diff paths encode their source session; remove this lookup when the API returns sessionId separately.
  for (let currentPage = 1; ; currentPage += 1) {
    const pageResult = await listSessions({
      projectId: target.projectId,
      page: currentPage,
      pageSize: 100,
      sort: 'updated_at desc',
    });
    if (generation !== requestGeneration.value || targetKey.value !== prefixTargetKey) return;
    loaded += pageResult.items.length;
    pageResult.items
      .filter((session) => session.branch === target.branch)
      .forEach((session) => {
        map[session.id.slice(0, 8)] = session.id;
        map[session.id] = session.id;
      });
    if (pageResult.items.length === 0 || loaded >= pageResult.pageInfo.total) break;
  }
  sessionPrefixMap.value = map;
}

function sessionPrefix(path: string) {
  const match = path.match(/^([^:]{1,64}):\s+/);
  return match?.[1] ?? '';
}

function filePathWithoutPrefix(path: string) {
  const prefix = sessionPrefix(path);
  return prefix ? path.slice(prefix.length + 2) : path;
}

async function openPrefixedSession(path: string) {
  const prefix = sessionPrefix(path);
  const sessionId = sessionPrefixMap.value[prefix];
  if (!sessionId) return;
  await router.push({ name: 'session-detail', params: { id: sessionId } });
}

function notifyError(err: unknown, fallback: string) {
  if (err && typeof err === 'object' && '__anycodeNotified' in err) return;
  Notify.create({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message: err instanceof Error ? err.message || fallback : fallback,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
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

watch(
  targetKey,
  () => {
    requestGeneration.value += 1;
    skipRequestSignature = '';
    collapseState.value = syncDiffCollapseTarget(collapseState.value, targetKey.value);
    diffContext.value = initialDiffContext();
    diff.value = null;
    sessionPrefixMap.value = {};
  },
  { immediate: true },
);

watch(
  () => requestSignature(),
  (signature) => {
    if (signature === skipRequestSignature) {
      skipRequestSignature = '';
      return;
    }
    void loadDiff();
  },
  { immediate: true },
);

onUnmounted(() => {
  requestGeneration.value += 1;
});
</script>

<style scoped>
.diff-workspace {
  display: flex;
  min-width: 0;
  min-height: 0;
  flex-direction: column;
  gap: 12px;
}

.diff-workspace__toolbar {
  display: flex;
  min-width: 0;
  flex: 0 0 auto;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.diff-workspace__page-size {
  width: 132px;
}

.diff-workspace__layout {
  display: grid;
  min-width: 0;
  grid-template-columns: 320px minmax(0, 1fr);
  align-items: start;
  gap: 16px;
}

.diff-workspace__files-header,
.files-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.diff-workspace__files-header {
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

.session-prefix-link {
  min-height: 24px;
  padding: 0 6px;
  font-family: ui-monospace, SFMono-Regular, Consolas, 'Liberation Mono', monospace;
}

@media (max-width: 1023.98px) {
  .diff-workspace__layout {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 599.98px) {
  .diff-workspace__page-size,
  .diff-workspace__toolbar :deep(.q-btn-toggle) {
    width: 100%;
  }
}
</style>
