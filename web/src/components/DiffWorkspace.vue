<template>
  <div class="diff-workspace">
    <!-- GLUE: standalone Diff owns these controls; the target only moves them into the app layout. -->
    <Teleport defer :to="toolbarTarget || 'body'" :disabled="!toolbarTarget">
      <div
        v-if="toolbarTitle || showFileNavigation || workspaceMode === 'all' || showRefresh"
        class="diff-workspace__toolbar"
        :class="{ 'diff-workspace__toolbar--header': toolbarTarget }"
      >
        <q-toolbar-title v-if="toolbarTitle" class="app-header__title diff-workspace__title">
          {{ toolbarTitle }}
        </q-toolbar-title>
        <q-btn-toggle
          v-if="showFileNavigation"
          :model-value="workspaceMode"
          no-caps
          unelevated
          toggle-color="primary"
          :disable="loading"
          :options="modeOptions"
          @update:model-value="setMode"
        />
        <q-space />
        <template v-if="workspaceMode === 'all'">
          <q-btn
            flat
            round
            dense
            icon="unfold_more"
            aria-label="展开全部文件"
            :disable="loading || allFilePaths.length === 0 || !hasCollapsedFile"
            @click="expandAllFiles"
          >
            <q-tooltip>展开全部文件</q-tooltip>
          </q-btn>
          <q-btn
            flat
            round
            dense
            icon="unfold_less"
            aria-label="折叠全部文件"
            :disable="loading || allFilePaths.length === 0 || allFilesCollapsed"
            @click="collapseAllFiles"
          >
            <q-tooltip>折叠全部文件</q-tooltip>
          </q-btn>
        </template>
        <q-btn
          v-if="showRefresh"
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
    </Teleport>

    <q-banner v-if="error" rounded class="diff-workspace__error app-feedback app-feedback--danger">
      <template #avatar>
        <q-icon name="error_outline" />
      </template>
      {{ error }}
      <template #action>
        <q-btn
          flat
          dense
          no-caps
          icon="refresh"
          label="重试"
          :loading="loading"
          @click="loadDiff"
        />
      </template>
    </q-banner>

    <q-banner
      v-if="diff && !diff.available"
      rounded
      class="state-banner app-feedback app-feedback--neutral"
    >
      <template #avatar>
        <q-icon name="block" />
      </template>
      当前范围没有可用 Diff，可能是非 git 项目、项目当前未检出该分支，或没有工作区变更。
    </q-banner>

    <div
      v-if="!diff || diff.available"
      class="diff-workspace__layout"
      :class="{ 'diff-workspace__layout--content-only': !showFileNavigation }"
    >
      <q-card v-if="showFileNavigation" flat bordered class="diff-files">
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
      </q-card>

      <section class="diff-content">
        <q-card v-if="loading && !diff" flat bordered class="diff-state-card">
          <q-card-section class="empty-state">
            <q-spinner color="primary" size="32px" />
            <div class="text-body2 text-muted">正在读取 Diff</div>
          </q-card-section>
        </q-card>

        <q-card v-else-if="!hasVisibleFiles" flat bordered class="diff-state-card">
          <q-card-section class="empty-state">
            <q-icon name="data_object" size="32px" class="text-muted" />
            <div class="text-body2">当前范围没有可展示的 Diff</div>
          </q-card-section>
        </q-card>

        <DiffViewer
          :files="visibleFiles"
          :file-diffs="visibleDiffs"
          :collapsible="workspaceMode === 'all'"
          :show-file-headers="showFileHeaders"
          :collapsed-paths="collapseState.collapsedPaths"
          :loading-paths="fileLoadingPaths"
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
              <span class="file-path">{{ filePathWithoutPrefix(file.path) }}</span>
            </template>
          </template>
        </DiffViewer>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRouter } from 'vue-router';

import DiffViewer from '@/components/DiffViewer.vue';
import {
  getBranchAllDiff,
  getBranchSingleDiff,
  getSessionAllDiff,
  getSessionDiffFiles,
  getSessionSingleDiff,
} from '@/services/diff';
import {
  collapseDiffFiles,
  expandDiffFiles,
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

const props = withDefaults(
  defineProps<{
    target: DiffWorkspaceTarget;
    modelValue: DiffWorkspaceState;
    showFileNavigation?: boolean;
    showFileHeaders?: boolean;
    showRefresh?: boolean;
    lazyFileDetails?: boolean;
    refreshKey?: string | number;
    toolbarTarget?: string;
    toolbarTitle?: string;
  }>(),
  {
    showFileNavigation: true,
    showFileHeaders: true,
    showRefresh: true,
    lazyFileDetails: false,
  },
);

const emit = defineEmits<{
  'update:modelValue': [state: DiffWorkspaceState];
}>();

const $q = useQuasar();
const router = useRouter();
const diff = ref<SessionDiff | null>(null);
const loading = ref(false);
const error = ref('');
const sessionPrefixMap = ref<Record<string, string>>({});
const diffContext = ref(initialDiffContext());
const requestGeneration = ref(0);
const fileLoadingPaths = ref<string[]>([]);
let skipRequestSignature = '';

const targetKey = computed(() =>
  props.target.kind === 'session'
    ? `session:${props.target.sessionId}`
    : `branch:${props.target.projectId}:${props.target.branch}`,
);
const collapseState = ref(initialDiffCollapseState(targetKey.value));
const workspaceMode = computed<DiffMode>(() => props.modelValue.mode);
const modeOptions = computed(() => [
  {
    ...($q.screen.lt.sm ? {} : { label: '单个文件' }),
    value: 'single',
    icon: 'description',
    'aria-label': '单个文件',
  },
  {
    ...($q.screen.lt.sm ? {} : { label: '全部 Diff' }),
    value: 'all',
    icon: 'difference',
    'aria-label': '全部 Diff',
  },
]);
const metadataFirst = computed(
  () => props.lazyFileDetails && props.target.kind === 'session' && workspaceMode.value === 'all',
);
const visibleDiffs = computed<FileDiff[]>(() => {
  if (!diff.value?.available) return [];
  return workspaceMode.value === 'all'
    ? diff.value.allDiff
    : diff.value.fileDiff
      ? [diff.value.fileDiff]
      : [];
});
const visibleFiles = computed<DiffFile[]>(() =>
  metadataFirst.value && diff.value?.available ? diff.value.files : [],
);
const hasVisibleFiles = computed(
  () => visibleFiles.value.length > 0 || visibleDiffs.value.length > 0,
);
const allFilePaths = computed(() => diff.value?.files.map((file) => file.path) ?? []);
const hasCollapsedFile = computed(() =>
  allFilePaths.value.some((path) =>
    isDiffFileCollapsed(collapseState.value, workspaceMode.value, path),
  ),
);
const allFilesCollapsed = computed(
  () =>
    allFilePaths.value.length > 0 &&
    allFilePaths.value.every((path) =>
      isDiffFileCollapsed(collapseState.value, workspaceMode.value, path),
    ),
);
const fileCountLabel = computed(() => {
  if (!diff.value) return '等待加载';
  return `共 ${diff.value.files.length} 个文件`;
});

function updateState(patch: Partial<DiffWorkspaceState>) {
  const next = { ...props.modelValue, ...patch };
  if (next.mode === props.modelValue.mode && next.filePath === props.modelValue.filePath) {
    return;
  }
  emit('update:modelValue', next);
}

function setMode(value: DiffMode) {
  updateState({ mode: value });
}

function selectFile(path: string) {
  updateState({ mode: 'single', filePath: path });
}

function requestSignature(state = props.modelValue) {
  const mode = props.showFileNavigation ? state.mode : 'all';
  return [targetKey.value, mode, state.filePath].join('|');
}

async function loadDiff() {
  const generation = ++requestGeneration.value;
  loading.value = true;
  error.value = '';
  try {
    const input = {
      mode: workspaceMode.value,
      contextBefore: diffContext.value.before,
      contextAfter: diffContext.value.after,
      ...(workspaceMode.value === 'single' && props.modelValue.filePath
        ? { filePath: props.modelValue.filePath }
        : {}),
    };
    const nextDiff =
      metadataFirst.value && props.target.kind === 'session'
        ? await getSessionDiffFiles({ sessionId: props.target.sessionId })
        : await requestDiff(input);
    if (generation !== requestGeneration.value) return;
    diff.value = nextDiff;
    if (metadataFirst.value) {
      collapseState.value = collapseDiffFiles(
        initialDiffCollapseState(targetKey.value),
        'all',
        nextDiff.files.map((file) => file.path),
      );
      fileLoadingPaths.value = [];
    }
    const normalizedState: DiffWorkspaceState = {
      ...props.modelValue,
      mode: workspaceMode.value,
      filePath:
        workspaceMode.value === 'single'
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
    if (generation === requestGeneration.value) error.value = errorMessage(err, '读取 Diff 失败');
  } finally {
    if (generation === requestGeneration.value) loading.value = false;
  }
}

function requestDiff(input: {
  mode: DiffMode;
  filePath?: string;
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

function expandDiff(filePath: string, direction: 'before' | 'after') {
  diffContext.value = expandDiffContext(diffContext.value, direction);
  if (metadataFirst.value) {
    void loadFileDiff(filePath);
    return;
  }
  void loadDiff();
}

async function toggleFileCollapsed(filePath: string) {
  if (loading.value) return;
  if (metadataFirst.value && isDiffFileCollapsed(collapseState.value, 'all', filePath)) {
    if (!(await loadFileDiff(filePath))) return;
  }
  collapseState.value = toggleDiffFileCollapsed(collapseState.value, workspaceMode.value, filePath);
}

function expandAllFiles() {
  if (metadataFirst.value) {
    void loadAllDiff();
    return;
  }
  collapseState.value = expandDiffFiles(
    collapseState.value,
    workspaceMode.value,
    allFilePaths.value,
  );
}

async function loadFileDiff(filePath: string) {
  if (props.target.kind !== 'session' || fileLoadingPaths.value.includes(filePath)) return false;
  const generation = requestGeneration.value;
  fileLoadingPaths.value = [...fileLoadingPaths.value, filePath];
  error.value = '';
  try {
    const nextDiff = await getSessionSingleDiff({
      sessionId: props.target.sessionId,
      mode: 'single',
      filePath,
      contextBefore: diffContext.value.before,
      contextAfter: diffContext.value.after,
    });
    if (generation !== requestGeneration.value || !nextDiff.fileDiff || !diff.value) return false;
    const loadedDiffs = diff.value.allDiff.filter((item) => item.file.path !== filePath);
    diff.value = {
      ...diff.value,
      fileDiff: nextDiff.fileDiff,
      allDiff: [...loadedDiffs, nextDiff.fileDiff],
    };
    return true;
  } catch (err) {
    if (generation === requestGeneration.value) {
      error.value = errorMessage(err, '读取文件变更失败');
    }
    return false;
  } finally {
    fileLoadingPaths.value = fileLoadingPaths.value.filter((path) => path !== filePath);
  }
}

async function loadAllDiff() {
  const generation = ++requestGeneration.value;
  loading.value = true;
  error.value = '';
  try {
    const nextDiff = await requestDiff({
      mode: 'all',
      contextBefore: diffContext.value.before,
      contextAfter: diffContext.value.after,
    });
    if (generation !== requestGeneration.value) return;
    diff.value = nextDiff;
    collapseState.value = expandDiffFiles(
      collapseState.value,
      'all',
      nextDiff.files.map((file) => file.path),
    );
  } catch (err) {
    if (generation === requestGeneration.value) error.value = errorMessage(err, '读取 Diff 失败');
  } finally {
    if (generation === requestGeneration.value) loading.value = false;
  }
}

function collapseAllFiles() {
  collapseState.value = collapseDiffFiles(
    collapseState.value,
    workspaceMode.value,
    allFilePaths.value,
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

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message || fallback : fallback;
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
    error.value = '';
    sessionPrefixMap.value = {};
    fileLoadingPaths.value = [];
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

watch(
  () => props.refreshKey,
  (value, previous) => {
    if (value !== previous) void loadDiff();
  },
);

onUnmounted(() => {
  requestGeneration.value += 1;
});
</script>

<style scoped>
.diff-workspace {
  container-type: inline-size;
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

.diff-workspace__toolbar--header {
  width: 100%;
  flex-wrap: nowrap;
}

.diff-workspace__toolbar--header .diff-workspace__title {
  max-width: min(32vw, 320px);
  flex: 0 1 auto;
}

.diff-workspace__error {
  flex: 0 0 auto;
}

.diff-workspace__layout {
  display: grid;
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  grid-template-columns: 320px minmax(0, 1fr);
  grid-template-rows: minmax(0, 1fr);
  align-items: stretch;
  gap: 16px;
}

.diff-workspace__layout--content-only {
  grid-template-columns: minmax(0, 1fr);
}

.diff-workspace__files-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
}

.file-path {
  min-width: 0;
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

.diff-content {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
}

.session-prefix-link {
  min-height: 24px;
  padding: 0 6px;
  font-family: ui-monospace, SFMono-Regular, Consolas, 'Liberation Mono', monospace;
}

@media (min-width: 1024px) {
  .diff-files {
    height: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
}

@container (max-width: 1023px) {
  .diff-workspace__layout {
    grid-template-columns: 1fr;
    grid-template-rows: minmax(120px, 35%) minmax(0, 1fr);
  }

  .diff-workspace__layout--content-only {
    grid-template-rows: minmax(0, 1fr);
  }

  .diff-files {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
}

@container (max-width: 599px) {
  .diff-workspace__toolbar:not(.diff-workspace__toolbar--header) :deep(.q-btn-toggle) {
    width: 100%;
  }
}

@media (max-width: 599.98px) {
  .diff-workspace__toolbar--header {
    gap: 4px;
  }

  .diff-workspace__toolbar--header .diff-workspace__title {
    display: none;
  }
}
</style>
