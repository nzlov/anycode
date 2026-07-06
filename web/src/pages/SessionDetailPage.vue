<template>
  <q-page class="page-shell detail-page">
    <div class="detail-grid">
      <section class="event-panel">
        <q-card flat bordered class="stream-card">
          <q-inner-loading :showing="loading">
            <q-spinner color="primary" size="32px" />
          </q-inner-loading>

          <div ref="streamBodyRef" class="stream-card__body" @scroll="onEventScroll">
            <div v-if="loadingOlderEvents" class="event-loading-more">
              <q-spinner color="primary" size="18px" />
              <span>正在加载更早事件</span>
            </div>
            <q-card-section v-if="!loading && streamEntries.length === 0" class="text-muted">
              暂无会话事件
            </q-card-section>

            <div class="event-list">
              <SessionEventMessage
                v-for="event in streamEntries"
                :key="event.id"
                :event="event"
              />
            </div>
          </div>
        </q-card>

        <div class="detail-composer">
          <q-card v-if="isWaitingForAnswer" flat bordered class="detail-answer-card">
            <q-card-section class="detail-answer-card__header">
              <div>
                <div class="text-subtitle2 text-weight-bold">待回答问题</div>
                <div class="text-caption text-muted">
                  回答后当前会话继续执行，输入框会恢复为追加描述。
                </div>
              </div>
              <q-badge rounded color="warning" text-color="dark" label="待回答" />
            </q-card-section>
            <q-separator />
            <AnswerUserPanel
              :batches="pendingQuestionBatches"
              :loading="questionsLoading"
              :submitting="questionsSubmitting"
              @submit="submitAnswers"
            />
          </q-card>
          <PromptComposer
            v-else
            v-model:prompt="appendText"
            v-model:files="appendFiles"
            v-model:model="composerModel"
            v-model:effort="composerEffort"
            v-model:permission="composerPermission"
            compact
            readonly-config
            :show-badge="false"
            title="追加描述"
            placeholder="追加描述，发送给当前会话"
            :disabled="!session || appending || stopping"
          >
            <template #actions>
              <q-btn
                v-if="composerAction"
                unelevated
                class="detail-composer__primary-btn"
                :color="composerAction.color"
                :icon="composerAction.icon"
                :aria-label="composerAction.tooltip"
                :loading="composerAction.loading"
                :disable="composerAction.disabled"
                @click="composerAction.run"
              >
                <q-tooltip>{{ composerAction.tooltip }}</q-tooltip>
              </q-btn>
            </template>
          </PromptComposer>
        </div>
      </section>

      <aside class="right-panel">
        <q-card flat bordered class="right-panel-card">
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
                    <q-item-label caption>标题</q-item-label>
                    <q-item-label>{{ session?.title ?? '会话详情' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>项目</q-item-label>
                    <q-item-label>{{ session?.projectName ?? '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>分支</q-item-label>
                    <q-item-label>{{ session?.branch ?? '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>更新时间</q-item-label>
                    <q-item-label>{{ session?.updatedAt ?? '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>模式</q-item-label>
                    <q-item-label>{{ session ? modeLabel(session.mode) : '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item>
                  <q-item-section>
                    <q-item-label caption>优先级</q-item-label>
                    <q-item-label>{{
                      session ? priorityLabel(session.priority) : '-'
                    }}</q-item-label>
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
                    <q-item-label caption>权限</q-item-label>
                    <q-item-label>{{ session?.config.permissionMode || '-' }}</q-item-label>
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
              <q-btn
                class="full-width q-mb-md"
                outline
                color="primary"
                icon="open_in_new"
                label="完整 Diff"
                no-caps
                :to="allDiffRoute"
              />

              <q-banner
                v-if="diff && !diff.available"
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
          <q-btn flat round dense icon="close" aria-label="关闭" v-close-popup>
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />

        <q-card-section v-if="fileDiffLoading" class="state-content">
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
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import { Notify } from 'quasar';
import { useRoute } from 'vue-router';

import AnswerUserPanel from '@/components/AnswerUserPanel.vue';
import PromptComposer from '@/components/PromptComposer.vue';
import SessionEventMessage, {
  type SessionEventMessageEntry,
} from '@/components/SessionEventMessage.vue';
import {
  firstCodexModelValue,
  normalizeCodexModel,
  normalizePermissionMode,
  normalizeReasoningEffort,
} from '@/components/promptOptions';
import { useSessionDetail } from '@/composables/useSessionDetail';
import { getSessionDiff } from '@/services/diff';
import type { DiffFile, DiffLineKind, FileDiff, SessionDiff } from '@/services/diff';
import type { QuestionAnswerInput, SessionMode, SessionStatus } from '@/services/sessions';

const route = useRoute();
const sessionId = String(route.params.id ?? '');
const appendText = ref('');
const streamBodyRef = ref<HTMLElement | null>(null);
const appendFiles = ref<File[]>([]);
const composerModel = ref(firstCodexModelValue());
const composerEffort = ref(normalizeReasoningEffort(composerModel.value, ''));
const composerPermission = ref(normalizePermissionMode('workspace-write'));
const tab = ref('info');
const diff = ref<SessionDiff | null>(null);
const diffLoading = ref(false);
const fileDiffDialog = ref(false);
const selectedFilePath = ref('');
const selectedFileDiff = ref<FileDiff | null>(null);
const fileDiffLoading = ref(false);
const diffPage = ref(1);
const diffPageSize = 20;
const {
  session,
  events,
  pendingQuestionBatches,
  loading,
  loadingOlderEvents,
  appending,
  starting,
  resuming,
  stopping,
  questionsLoading,
  questionsSubmitting,
  loadSessionDetail,
  appendDescription,
  startSession,
  resumeSession,
  stopSession,
  loadPendingQuestions,
  loadOlderEvents,
  submitPendingAnswers,
  startLiveUpdates,
  stopLiveUpdates,
} = useSessionDetail(sessionId);

const canRun = computed(() => session.value?.availableActions.includes('run') ?? false);
const canResume = computed(() => session.value?.availableActions.includes('resume') ?? false);
const isWaitingForAnswer = computed(
  () => session.value?.pendingQuestion || session.value?.status === 'waiting_user',
);
type StreamEntry = SessionEventMessageEntry;

const streamEntries = computed<StreamEntry[]>(() => {
  const entries: StreamEntry[] = [];
  if (session.value) {
    entries.push({
      id: `session-input-${session.value.id}`,
      kind: 'user',
      title: '用户输入',
      body: session.value.summary,
      createdAt: session.value.createdAt,
      time: session.value.createdTime,
      rawType: 'user.input',
    });
    for (const item of session.value.promptAppends) {
      entries.push({
        id: `prompt-append-${item.id}`,
        kind: 'user',
        title: '追加描述',
        body: item.body,
        createdAt: item.createdAt,
        time: item.time,
        rawType: 'user.append',
      });
    }
  }
  for (const event of events.value) {
    entries.push(event);
  }
  const sortedEntries = entries.sort((left, right) => {
    const diff = Date.parse(left.createdAt) - Date.parse(right.createdAt);
    return diff === 0 ? left.id.localeCompare(right.id) : diff;
  });
  return dedupeStreamEntries(sortedEntries);
});
const composerAction = computed(() => {
  const current = session.value;
  if (!current) return null;
  if (appendText.value.trim().length > 0) {
    return {
      icon: 'send',
      color: 'primary',
      tooltip: '发送追加描述',
      loading: appending.value,
      disabled: stopping.value,
      run: sendAppend,
    };
  }
  if (current.status === 'starting' || current.status === 'running') {
    return {
      icon: 'stop',
      color: 'negative',
      tooltip: '运行中，点击停止',
      loading: stopping.value,
      disabled: appending.value || starting.value || resuming.value,
      run: stopSession,
    };
  }
  if (current.status === 'stopping') {
    return {
      icon: 'hourglass_top',
      color: 'warning',
      tooltip: '停止中',
      loading: stopping.value,
      disabled: true,
      run: stopSession,
    };
  }
  if (canRun.value) {
    return {
      icon: 'play_arrow',
      color: 'positive',
      tooltip: '强制运行',
      loading: starting.value,
      disabled: appending.value || resuming.value || stopping.value,
      run: startSession,
    };
  }
  if (canResume.value) {
    return {
      icon: 'restart_alt',
      color: 'primary',
      tooltip: '恢复会话',
      loading: resuming.value,
      disabled: appending.value || starting.value || stopping.value,
      run: resumeSession,
    };
  }
  return {
    icon: 'pause_circle',
    color: 'grey-7',
    tooltip: '已暂停',
    loading: false,
    disabled: true,
    run: startSession,
  };
});
const workflowProgressIndeterminate = computed(() =>
  ['queued', 'starting', 'running', 'waiting_user', 'waiting_approval', 'stopping'].includes(
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

function dedupeStreamEntries(entries: StreamEntry[]) {
  const hasCodexProcessExit = entries.some(
    (event) => event.rawType === 'process.codex_event' && event.title === '进程退出',
  );
  const result: StreamEntry[] = [];
  for (const event of entries) {
    if (hasCodexProcessExit && event.rawType === 'process.exited') {
      continue;
    }
    if (event.title === '进程退出' && !event.body) {
      continue;
    }
    const previous = result[result.length - 1];
    if (
      previous &&
      event.kind === 'status' &&
      previous.kind === 'status' &&
      event.title === previous.title &&
      event.body === previous.body &&
      Math.abs(Date.parse(event.createdAt) - Date.parse(previous.createdAt)) < 1500
    ) {
      continue;
    }
    result.push(event);
  }
  return result;
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function priorityLabel(priority: 'high' | 'medium' | 'low') {
  const labels: Record<'high' | 'medium' | 'low', string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
  };
  return labels[priority];
}

function statusColor(value: SessionStatus) {
  const colors: Record<SessionStatus, string> = {
    created: 'blue-grey',
    queued: 'warning',
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
    queued: '排队中',
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
  try {
    diff.value = await getSessionDiff({
      sessionId,
      mode: 'single',
      page: diffPage.value,
      pageSize: diffPageSize,
    });
    diffPage.value = diff.value.pageInfo.page;
  } catch (err) {
    notifyError(err, '读取 Diff 失败');
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

async function loadFileDiff(path: string) {
  fileDiffLoading.value = true;
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
    notifyError(err, '读取文件 Diff 失败');
  } finally {
    fileDiffLoading.value = false;
  }
}

function notifyError(err: unknown, fallback: string) {
  if (wasNotified(err)) return;
  Notify.create({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message: err instanceof Error ? err.message || fallback : fallback,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
}

function wasNotified(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
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
  appendFiles.value = [];
}

async function submitAnswers(batchId: string, answers: QuestionAnswerInput[]) {
  await submitPendingAnswers(batchId, answers);
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

watch(
  () => loading.value,
  (value, previous) => {
    if (!value && previous) {
      void scrollEventsToBottom();
    }
  },
);

watch(
  () => events.value.length,
  (_value, previous) => {
    if (loadingOlderEvents.value) return;
    const body = streamBodyRef.value;
    const shouldStickToBottom =
      !body || previous === 0 || body.scrollHeight - body.scrollTop - body.clientHeight < 96;
    if (shouldStickToBottom) {
      void scrollEventsToBottom();
    }
  },
);

watch(
  isWaitingForAnswer,
  (value) => {
    if (value) {
      void loadPendingQuestions();
    }
  },
  { immediate: true },
);

watch(
  session,
  (value) => {
    if (!value) return;
    const nextModel = normalizeCodexModel(value.config.codexModel);
    composerModel.value = nextModel;
    composerEffort.value = normalizeReasoningEffort(nextModel, value.config.reasoningEffort);
    composerPermission.value = normalizePermissionMode(value.config.permissionMode);
  },
  { immediate: true },
);

onMounted(() => {
  void loadSessionDetail();
  void loadPendingQuestions();
  startLiveUpdates();
});

onUnmounted(() => {
  stopLiveUpdates();
});

async function onEventScroll() {
  const body = streamBodyRef.value;
  if (!body || body.scrollTop > 64 || loadingOlderEvents.value) return;
  const previousHeight = body.scrollHeight;
  await loadOlderEvents();
  await nextTick();
  body.scrollTop = body.scrollHeight - previousHeight + body.scrollTop;
}

async function scrollEventsToBottom() {
  await nextTick();
  const body = streamBodyRef.value;
  if (!body) return;
  body.scrollTop = body.scrollHeight;
}
</script>

<style scoped>
.detail-page {
  box-sizing: border-box;
  display: flex;
  height: 100%;
  max-height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.detail-page .detail-grid {
  flex: 1 1 auto;
  min-height: 0;
  align-items: stretch;
}

.event-panel {
  display: grid;
  min-height: 0;
  height: 100%;
  gap: 0;
  grid-template-rows: minmax(0, 1fr) auto;
}

.stream-card {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
  border-bottom-right-radius: 0;
  border-bottom-left-radius: 0;
}

.stream-card__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  padding: 0 14px 14px;
}

.event-loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px;
  color: var(--ac-text-muted);
  font-size: 12px;
}

.event-list {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.right-panel,
.right-panel-card {
  min-height: 0;
  height: 100%;
}

.right-panel-card {
  display: flex;
  height: 100%;
  flex-direction: column;
  overflow: hidden;
}

.right-panel-card :deep(.q-tabs) {
  flex: 0 0 auto;
}

.right-panel-card :deep(.q-tab-panels) {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}

.detail-composer {
  display: flex;
  min-height: 208px;
  flex-direction: column;
  padding: 0;
  background: var(--ac-surface-raised);
  border-top-right-radius: 0;
  border-top-left-radius: 0;
}

.detail-answer-card {
  max-height: min(52vh, 520px);
  overflow: auto;
  border-color: var(--ac-border);
  background: var(--ac-surface-raised);
}

.detail-answer-card__header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 12px 14px;
  background: var(--ac-surface-muted);
}

.detail-composer__header {
  padding: 10px 14px;
}

.detail-composer__input {
  flex: 1 1 auto;
  padding: 10px 12px 6px;
}

.detail-composer__hint {
  margin-bottom: 6px;
  color: var(--ac-text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.detail-composer__input .prompt-input {
  min-height: 96px;
}

.detail-composer__input .prompt-input :deep(.q-field__control) {
  min-height: 96px;
  background: transparent;
}

.detail-composer__input .prompt-input :deep(.q-field__native) {
  min-height: 72px;
}

.detail-composer__actions {
  flex: 0 0 auto;
  flex-wrap: wrap;
  gap: 8px;
  padding: 8px 12px;
  background: color-mix(in srgb, var(--ac-surface-muted) 68%, transparent);
}

.detail-composer__icon-btn {
  width: 42px;
  min-width: 42px;
  height: 30px;
  min-height: 30px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  color: var(--q-primary);
}

.detail-composer__select {
  max-width: 180px;
  background: var(--ac-surface-muted);
  color: var(--ac-text);
}

.detail-composer__primary-btn {
  width: 42px;
  min-width: 42px;
  height: 42px;
  min-height: 42px;
  border-radius: 11px;
}

.detail-composer__status {
  display: inline-flex;
  flex: 1 1 180px;
  min-width: 0;
  align-items: center;
  gap: 6px;
  justify-content: flex-end;
  color: var(--ac-text-muted);
  font-size: 12px;
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
  .detail-page {
    height: 100%;
  }

  .detail-page .detail-grid {
    gap: 12px;
  }

  .stream-card__body {
    padding: 0 10px 10px;
  }

  .right-panel {
    display: none;
  }

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
