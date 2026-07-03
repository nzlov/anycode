<template>
  <q-page class="page-shell detail-page">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">{{ session?.title ?? '会话详情' }}</div>
        <div class="text-body2 text-muted">
          <template v-if="session">
            {{ session.projectId }} · {{ session.branch }} · {{ session.updatedAt }}
          </template>
          <template v-else>{{ sessionId }}</template>
        </div>
      </div>
      <div class="row q-gutter-sm">
        <q-btn
          v-if="session?.pendingQuestion"
          unelevated
          color="warning"
          text-color="dark"
          icon="help"
          label="回答问题"
          no-caps
          :loading="questionsLoading"
          @click="openAnswerDialog"
        />
        <q-btn
          outline
          color="primary"
          icon="difference"
          label="完整 Diff"
          no-caps
          :to="allDiffRoute"
        />
        <q-btn
          v-if="canResume"
          outline
          color="primary"
          icon="restart_alt"
          label="恢复"
          no-caps
          :loading="resuming"
          :disable="starting || stopping"
          @click="resumeSession"
        />
        <q-btn
          v-if="canRun"
          unelevated
          color="positive"
          icon="play_arrow"
          :label="runActionLabel"
          no-caps
          :loading="starting"
          :disable="resuming || stopping"
          @click="startSession"
        />
        <q-btn
          v-if="canStop"
          unelevated
          color="negative"
          icon="stop"
          label="停止"
          no-caps
          :loading="stopping"
          @click="stopSession"
        />
        <q-btn
          v-if="canClose"
          outline
          color="grey-8"
          icon="close"
          label="关闭"
          no-caps
          :loading="closing"
          :disable="starting || resuming || stopping"
          @click="closeSession"
        />
      </div>
    </div>

    <div class="detail-grid">
      <section class="event-panel">
        <q-card flat bordered class="stream-card">
          <q-inner-loading :showing="loading">
            <q-spinner color="primary" size="32px" />
          </q-inner-loading>

          <q-banner v-if="error" dense class="bg-negative text-white">
            {{ error }}
          </q-banner>

          <q-card-section v-if="!loading && !error && events.length === 0" class="text-muted">
            暂无会话事件
          </q-card-section>

          <q-card-section v-for="event in events" :key="event.id" class="event-item">
            <div class="event-icon">
              <q-icon :name="eventIcon(event.kind)" />
            </div>
            <div class="event-body">
              <div class="row items-center q-gutter-sm">
                <div class="text-weight-medium">{{ event.title }}</div>
                <span class="text-caption text-muted">{{ event.time }}</span>
              </div>
              <div class="text-body2">{{ event.body }}</div>
            </div>
          </q-card-section>
        </q-card>

        <q-card flat bordered class="composer-card prompt-shell detail-composer">
          <q-input
            v-model.trim="appendText"
            autogrow
            borderless
            type="textarea"
            class="prompt-input"
            placeholder="追加描述，发送给当前会话"
            :disable="!session || appending || stopping"
          />
          <q-card-actions align="right">
            <q-btn
              v-if="canStop"
              unelevated
              color="negative"
              icon="stop"
              label="停止"
              no-caps
              :loading="stopping"
              :disable="appending || starting || resuming"
              @click="stopSession"
            />
            <q-btn
              v-else
              unelevated
              color="primary"
              icon="send"
              label="发送"
              no-caps
              :loading="appending"
              :disable="!session || stopping || appendText.trim().length === 0"
              @click="sendAppend"
            />
          </q-card-actions>
        </q-card>
      </section>

      <aside class="right-panel">
        <q-card flat bordered>
          <q-tabs v-model="tab" dense align="justify" narrow-indicator>
            <q-tab name="info" icon="info" label="会话信息" />
            <q-tab name="changes" icon="difference" label="当前变更" />
          </q-tabs>
          <q-separator />
          <q-tab-panels v-model="tab" animated>
            <q-tab-panel name="info">
              <div v-if="session?.mode === 'workflow'" class="workflow-progress">
                <div class="workflow-progress__header">
                  <div>
                    <div class="text-subtitle2 text-weight-bold">流程进度</div>
                    <div class="text-caption text-muted">{{ workflowProgressLabel }}</div>
                  </div>
                  <q-badge
                    outline
                    :color="statusColor(session.status)"
                    :label="statusLabel(session.status)"
                  />
                </div>
                <q-linear-progress
                  rounded
                  size="8px"
                  :indeterminate="workflowProgressIndeterminate"
                  :value="workflowProgressValue"
                  :color="statusColor(session.status)"
                />
                <div class="workflow-progress__node">
                  <q-icon name="account_tree" color="primary" />
                  <span>{{ session.node }}</span>
                </div>
              </div>

              <q-list separator>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模式</q-item-label>
                    <q-item-label>{{ session ? modeLabel(session.mode) : '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>当前节点</q-item-label>
                    <q-item-label>{{ session?.node ?? '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>状态</q-item-label>
                    <q-item-label>
                      <q-badge
                        v-if="session"
                        outline
                        :color="statusColor(session.status)"
                        :label="statusLabel(session.status)"
                      />
                      <template v-else>-</template>
                    </q-item-label>
                  </q-item-section>
                </q-item>
                <q-item v-if="session?.closeReason">
                  <q-item-section>
                    <q-item-label caption>关闭原因</q-item-label>
                    <q-item-label>{{ closeReasonLabel(session.closeReason) }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模型</q-item-label>
                    <q-item-label>{{ modelLabel }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>权限</q-item-label>
                    <q-item-label>{{ session?.config.permissionMode || '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>思考强度</q-item-label>
                    <q-item-label>{{ session?.config.reasoningEffort || '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>

              <q-separator spaced />

              <div class="append-history">
                <div class="append-history__title">追加描述</div>
                <q-list v-if="session?.promptAppends.length" bordered separator>
                  <q-item v-for="item in session.promptAppends" :key="item.id">
                    <q-item-section>
                      <q-item-label class="append-history__body">{{ item.body }}</q-item-label>
                      <q-item-label caption>{{ item.time }}</q-item-label>
                    </q-item-section>
                  </q-item>
                </q-list>
                <div v-else class="append-history__empty">暂无追加描述</div>
              </div>
            </q-tab-panel>

            <q-tab-panel name="changes">
              <q-banner v-if="diffError" dense class="state-block bg-negative text-white q-mb-md">
                <template #avatar>
                  <q-icon name="error" />
                </template>
                {{ diffError }}
                <template #action>
                  <q-btn
                    flat
                    color="white"
                    label="重试"
                    no-caps
                    :loading="diffLoading"
                    @click="loadChangeList"
                  />
                </template>
              </q-banner>

              <q-banner
                v-else-if="diff && !diff.available"
                dense
                class="state-block bg-grey-2 text-grey-8 q-mb-md"
              >
                <template #avatar>
                  <q-icon name="block" />
                </template>
                当前会话没有可用 worktree Diff，可能是非 git 项目或会话尚未创建 worktree。
              </q-banner>

              <q-list v-if="diff?.available" bordered separator class="changes-list">
                <q-item-label header class="changes-header">
                  <span>{{ fileCountLabel }}</span>
                  <q-btn
                    flat
                    round
                    dense
                    icon="refresh"
                    :loading="diffLoading"
                    @click="loadChangeList"
                  >
                    <q-tooltip>刷新 Diff</q-tooltip>
                  </q-btn>
                </q-item-label>

                <q-item v-if="diffLoading && !diff.files.length">
                  <q-item-section avatar>
                    <q-spinner color="primary" size="24px" />
                  </q-item-section>
                  <q-item-section>
                    <q-item-label>正在读取变更文件</q-item-label>
                  </q-item-section>
                </q-item>

                <q-item v-else-if="!diffLoading && diff.files.length === 0">
                  <q-item-section avatar>
                    <q-icon name="task_alt" color="positive" />
                  </q-item-section>
                  <q-item-section>
                    <q-item-label>暂无文件变更</q-item-label>
                  </q-item-section>
                </q-item>

                <q-item
                  v-for="file in diff.files"
                  :key="file.path"
                  clickable
                  :disable="fileDiffLoading"
                  @click="openFileDiff(file.path)"
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
                  <q-item-section side>
                    <q-icon name="chevron_right" />
                  </q-item-section>
                </q-item>
              </q-list>

              <q-pagination
                v-if="diff?.available && diffPageMax > 1"
                v-model="diffPage"
                dense
                boundary-numbers
                direction-links
                class="justify-center q-mt-md"
                :max="diffPageMax"
                :max-pages="5"
                :disable="diffLoading"
              />

              <q-card v-else-if="diffLoading" flat bordered class="state-card">
                <q-card-section class="state-content">
                  <q-spinner color="primary" size="28px" />
                  <div class="text-body2 text-muted">正在读取变更文件</div>
                </q-card-section>
              </q-card>

              <q-btn
                class="full-width q-mt-md"
                outline
                color="primary"
                icon="open_in_new"
                label="查看全部"
                no-caps
                :to="allDiffRoute"
              />
            </q-tab-panel>
          </q-tab-panels>
        </q-card>
      </aside>
    </div>

    <q-dialog v-model="fileDiffDialog">
      <q-card class="file-diff-dialog">
        <q-card-section class="diff-dialog-header">
          <div class="file-title">
            <q-icon
              v-if="selectedFileDiff"
              :name="fileIcon(selectedFileDiff.file.status)"
              :color="fileColor(selectedFileDiff.file.status)"
            />
            <span>{{ selectedFilePath || '文件 Diff' }}</span>
          </div>
          <q-btn flat round dense icon="close" v-close-popup>
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />

        <q-banner v-if="fileDiffError" dense class="bg-negative text-white q-ma-md">
          <template #avatar>
            <q-icon name="error" />
          </template>
          {{ fileDiffError }}
          <template #action>
            <q-btn
              flat
              color="white"
              label="重试"
              no-caps
              :loading="fileDiffLoading"
              @click="reloadSelectedFileDiff"
            />
          </template>
        </q-banner>

        <q-card-section v-else-if="fileDiffLoading" class="state-content">
          <q-spinner color="primary" size="32px" />
          <div class="text-body2 text-muted">正在读取文件 Diff</div>
        </q-card-section>

        <q-card-section v-else-if="!selectedFileDiff" class="state-content">
          <q-icon name="data_object" size="32px" color="grey-6" />
          <div class="text-body2">当前文件没有可展示的 Diff</div>
        </q-card-section>

        <q-card-section v-else class="diff-code">
          <div class="diff-file-meta">
            <q-badge outline color="positive" :label="`+${selectedFileDiff.file.additions}`" />
            <q-badge outline color="negative" :label="`-${selectedFileDiff.file.deletions}`" />
            <q-badge
              outline
              :color="fileColor(selectedFileDiff.file.status)"
              :label="selectedFileDiff.file.status"
            />
          </div>
          <div
            v-for="line in selectedFileDiff.lines"
            :key="`${selectedFileDiff.file.path}:${line.id}`"
            class="diff-line"
            :class="lineClass(line.kind)"
          >
            <span class="line-number">{{ line.oldLine ?? '' }}</span>
            <span class="line-number">{{ line.newLine ?? '' }}</span>
            <pre>{{ line.content }}</pre>
          </div>
        </q-card-section>
      </q-card>
    </q-dialog>

    <AnswerUserDialog
      v-model="answerDialog"
      :batches="pendingQuestionBatches"
      :loading="questionsLoading"
      :submitting="questionsSubmitting"
      @submit="submitAnswers"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import { useSessionDetail } from '@/composables/useSessionDetail';
import { getSessionDiff } from '@/services/diff';
import type { DiffFile, DiffLineKind, FileDiff, SessionDiff } from '@/services/diff';
import type { QuestionAnswerInput, SessionEvent, SessionMode, SessionStatus } from '@/services/sessions';

const route = useRoute();
const sessionId = String(route.params.id ?? '');
const appendText = ref('');
const tab = ref('info');
const diff = ref<SessionDiff | null>(null);
const diffLoading = ref(false);
const diffError = ref('');
const fileDiffDialog = ref(false);
const selectedFilePath = ref('');
const selectedFileDiff = ref<FileDiff | null>(null);
const fileDiffLoading = ref(false);
const fileDiffError = ref('');
const diffPage = ref(1);
const diffPageSize = 20;
const answerDialog = ref(false);
const {
  session,
  events,
  pendingQuestionBatches,
  loading,
  appending,
  starting,
  resuming,
  stopping,
  closing,
  questionsLoading,
  questionsSubmitting,
  error,
  loadSessionDetail,
  appendDescription,
  startSession,
  resumeSession,
  stopSession,
  closeSession,
  loadPendingQuestions,
  submitPendingAnswers,
  startLiveUpdates,
  stopLiveUpdates,
} = useSessionDetail(sessionId);

const canRun = computed(() => session.value?.availableActions.includes('run') ?? false);
const canResume = computed(() => session.value?.availableActions.includes('resume') ?? false);
const canStop = computed(() => session.value?.availableActions.includes('stop') ?? false);
const canClose = computed(() => session.value?.availableActions.includes('close') ?? false);
const runActionLabel = computed(() =>
  session.value?.status === 'resume_failed' ? '重新运行当前节点' : '运行',
);
const modelLabel = computed(() => session.value?.config.codexModel || 'Codex CLI 默认');
const workflowProgressIndeterminate = computed(() =>
  ['starting', 'running', 'waiting_user', 'waiting_approval', 'stopping'].includes(
    session.value?.status ?? '',
  ),
);
const workflowProgressValue = computed(() => {
  const status = session.value?.status;
  if (status === 'completed' || status === 'closed') return 1;
  if (status === 'failed' || status === 'blocked' || status === 'resume_failed') return 0.65;
  if (status === 'created') return 0.05;
  if (status === 'stopped') return 0.35;
  return 0.5;
});
const workflowProgressLabel = computed(() => {
  if (!session.value) return '等待会话加载';
  if (session.value.status === 'completed') return '流程已完成';
  if (session.value.status === 'closed') return '流程已关闭';
  if (session.value.status === 'blocked') return '流程已阻塞';
  if (session.value.status === 'resume_failed') return '等待恢复处理';
  if (session.value.status === 'waiting_approval') return '等待人工审批';
  if (session.value.status === 'waiting_user') return '等待用户回答';
  if (session.value.status === 'running' || session.value.status === 'starting') {
    return '当前节点运行中';
  }
  return '流程未运行';
});
const allDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId, mode: 'all' },
}));
const fileCountLabel = computed(() => {
  const info = diff.value?.pageInfo;
  if (!info) return '等待加载';
  return `第 ${info.page} 页，共 ${info.total} 个文件`;
});
const diffPageMax = computed(() => {
  const info = diff.value?.pageInfo;
  if (!info || info.total < 1) return 1;
  return Math.max(1, Math.ceil(info.total / info.pageSize));
});

function eventIcon(kind: SessionEvent['kind']) {
  const icons: Record<SessionEvent['kind'], string> = {
    thought: 'psychology',
    tool: 'terminal',
    assistant: 'smart_toy',
    status: 'radio_button_checked',
    question: 'help',
  };
  return icons[kind];
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function statusColor(value: SessionStatus) {
  const colors: Record<SessionStatus, string> = {
    created: 'blue-grey',
    starting: 'primary',
    running: 'positive',
    waiting_user: 'warning',
    waiting_approval: 'warning',
    stopping: 'warning',
    stopped: 'blue-grey',
    resume_failed: 'negative',
    failed: 'negative',
    blocked: 'negative',
    completed: 'primary',
    closed: 'grey',
  };
  return colors[value];
}

function statusLabel(value: SessionStatus) {
  const labels: Record<SessionStatus, string> = {
    created: '待运行',
    starting: '启动中',
    running: '运行中',
    waiting_user: '待回答',
    waiting_approval: '待审批',
    stopping: '停止中',
    stopped: '已停止',
    resume_failed: '恢复失败',
    failed: '失败',
    blocked: '阻塞',
    completed: '已完成',
    closed: '已关闭',
  };
  return labels[value];
}

function closeReasonLabel(value: string) {
  const labels: Record<string, string> = {
    user_closed: '用户关闭',
    merged_closed: '合并后关闭',
  };
  return labels[value] ?? value;
}

async function loadChangeList() {
  if (!sessionId) return;
  diffLoading.value = true;
  diffError.value = '';
  try {
    diff.value = await getSessionDiff({
      sessionId,
      mode: 'single',
      page: diffPage.value,
      pageSize: diffPageSize,
    });
    diffPage.value = diff.value.pageInfo.page;
  } catch (err) {
    diffError.value = err instanceof Error ? err.message : '读取 Diff 失败';
  } finally {
    diffLoading.value = false;
  }
}

async function openFileDiff(path: string) {
  selectedFilePath.value = path;
  selectedFileDiff.value = null;
  fileDiffDialog.value = true;
  await loadFileDiff(path);
}

async function reloadSelectedFileDiff() {
  if (!selectedFilePath.value) return;
  await loadFileDiff(selectedFilePath.value);
}

async function loadFileDiff(path: string) {
  fileDiffLoading.value = true;
  fileDiffError.value = '';
  try {
    const nextDiff = await getSessionDiff({
      sessionId,
      mode: 'single',
      filePath: path,
      page: diffPage.value,
      pageSize: diffPageSize,
    });
    selectedFileDiff.value = nextDiff.fileDiff;
  } catch (err) {
    fileDiffError.value = err instanceof Error ? err.message : '读取文件 Diff 失败';
  } finally {
    fileDiffLoading.value = false;
  }
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

async function sendAppend() {
  const text = appendText.value;
  await appendDescription(text);
  appendText.value = '';
}

async function openAnswerDialog() {
  await loadPendingQuestions();
  answerDialog.value = true;
}

async function submitAnswers(batchId: string, answers: QuestionAnswerInput[]) {
  await submitPendingAnswers(batchId, answers);
  answerDialog.value = pendingQuestionBatches.value.length > 0;
}

watch(tab, (value) => {
  if (value === 'changes' && !diff.value && !diffLoading.value) {
    void loadChangeList();
  }
});

watch(diffPage, () => {
  if (tab.value === 'changes') {
    void loadChangeList();
  }
});

onMounted(() => {
  void loadSessionDetail();
  void loadPendingQuestions();
  startLiveUpdates();
});

onUnmounted(() => {
  stopLiveUpdates();
});
</script>

<style scoped>
.detail-composer {
  grid-template-rows: minmax(120px, auto) auto;
}

.detail-composer :deep(.q-card__actions) {
  padding: 8px 12px 12px;
}

.state-block {
  border-radius: var(--ac-radius);
}

.workflow-progress {
  display: grid;
  gap: 12px;
  padding: 12px;
  margin-bottom: 12px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
}

.workflow-progress__header,
.workflow-progress__node {
  display: flex;
  align-items: center;
  gap: 8px;
}

.workflow-progress__header {
  justify-content: space-between;
}

.workflow-progress__node {
  min-width: 0;
  color: var(--ac-text);
  font-size: 13px;
}

.workflow-progress__node span {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.changes-list {
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.changes-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  color: var(--ac-text-muted);
}

.file-path {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.state-card {
  background: var(--ac-surface);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

.state-content {
  display: grid;
  min-height: 140px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--ac-text-muted);
  text-align: center;
}

.append-history {
  display: grid;
  gap: 10px;
}

.append-history__title {
  font-size: 13px;
  font-weight: 700;
  color: var(--ac-text);
}

.append-history__body {
  overflow-wrap: anywhere;
  word-break: break-word;
  white-space: pre-wrap;
}

.append-history__empty {
  color: var(--ac-text-muted);
  font-size: 13px;
}

.file-diff-dialog {
  width: min(1100px, 92vw);
  max-width: 92vw;
  max-height: 86vh;
  display: flex;
  flex-direction: column;
}

.diff-dialog-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.file-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}

.file-title span {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.diff-code {
  min-height: 0;
  overflow: auto;
  padding: 0;
}

.diff-file-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--ac-border);
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
  .file-diff-dialog {
    width: 100vw;
    max-width: 100vw;
    max-height: 100vh;
  }

  .diff-line {
    grid-template-columns: 44px 44px minmax(max-content, 1fr);
  }
}
</style>
