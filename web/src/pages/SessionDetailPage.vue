<template>
  <q-page class="page-shell detail-page">
    <q-tabs v-model="detailView" class="detail-mobile-tabs lt-md" dense align="justify">
      <q-tab name="session" icon="forum" label="会话" />
      <q-tab name="info" icon="info" label="信息" />
      <q-tab name="changes" icon="difference" label="变更" />
      <q-tab name="artifacts" icon="inventory_2" label="产物" />
    </q-tabs>

    <div class="detail-grid">
      <section
        class="event-panel"
        :class="{ 'event-panel--mobile-hidden': detailView !== 'session' }"
      >
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
              <div
                v-for="event in streamEntries"
                :key="event.id"
                class="event-list__item"
                :data-timeline-id="event.id"
              >
                <SessionEventMessage
                  :event="event"
                  :known-user-prompts="knownUserPrompts"
                  :workflow-prompt="session?.mode === 'workflow'"
                />
              </div>
            </div>
          </div>
        </q-card>

        <div v-if="!isClosed" class="detail-composer">
          <q-banner v-if="detailError" rounded class="detail-error-banner">
            <template #avatar>
              <q-icon name="error_outline" />
            </template>
            {{ detailError }}
          </q-banner>
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
          <div v-else-if="isWaitingForApproval" class="detail-approval-review">
            <div class="detail-approval-review__result">
              <WorkflowResultReview
                :phase="session?.pendingApproval?.phase ?? null"
                :result="session?.pendingApproval?.result ?? null"
              />
            </div>
            <WorkflowApprovalPanel
              :key="approvalPanelKey"
              :context-available="isPendingApprovalReviewable(session?.pendingApproval)"
              :submitting="approvalSubmitting"
              @submit="submitApproval"
            />
          </div>
          <CodexPromptComposer
            v-else
            v-model:prompt="appendText"
            v-model:files="appendFiles"
            v-model:model="composerModel"
            v-model:effort="composerEffort"
            v-model:permission="composerPermission"
            v-model:fast="composerFast"
            compact
            :show-badge="false"
            title="追加描述"
            placeholder="追加描述，发送给当前会话"
            :disabled="!session || appendUploading || appending || stopping || isClosed"
          >
            <template #actions>
              <q-btn
                v-if="canCancelQueue"
                flat
                class="app-icon-btn"
                color="negative"
                icon="cancel"
                aria-label="取消排队"
                :loading="stopping"
                :disable="appending || executing || stopping || updatingConfig"
                @click="stopSession"
              >
                <q-tooltip>取消排队</q-tooltip>
              </q-btn>
              <q-btn
                v-if="composerAction"
                unelevated
                class="detail-composer__primary-btn app-icon-btn"
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
          </CodexPromptComposer>
        </div>
      </section>

      <aside
        class="right-panel"
        :class="{ 'right-panel--mobile-hidden': detailView === 'session' }"
      >
        <q-card flat bordered class="right-panel-card">
          <q-tabs
            v-model="rightPanelTab"
            class="detail-desktop-tabs gt-sm"
            dense
            align="justify"
            narrow-indicator
          >
            <q-tab name="info" icon="info" label="会话信息" />
            <q-tab name="changes" icon="difference" label="当前变更" />
            <q-tab name="artifacts" icon="inventory_2" label="产物" />
          </q-tabs>
          <q-separator class="gt-sm" />
          <q-tab-panels v-model="rightPanelTab" animated>
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
                    <q-item-label caption>工作分支</q-item-label>
                    <q-item-label>{{ session?.worktreeBranch || '-' }}</q-item-label>
                  </q-item-section>
                </q-item>
                <q-item v-if="worktreeCleanup && worktreeCleanup.status !== 'not_applicable'">
                  <q-item-section>
                    <q-item-label caption>工作树清理</q-item-label>
                    <q-item-label>
                      <q-badge
                        outline
                        :color="worktreeCleanupColor(worktreeCleanup.status)"
                        :label="worktreeCleanupLabel(worktreeCleanup.status)"
                      />
                    </q-item-label>
                    <q-item-label v-if="worktreeCleanup.error" caption class="text-negative">
                      {{ worktreeCleanup.error.message }}
                    </q-item-label>
                  </q-item-section>
                  <q-item-section v-if="canRetryWorktreeCleanup" side>
                    <q-btn
                      flat
                      round
                      dense
                      icon="refresh"
                      color="primary"
                      aria-label="重试工作树清理"
                      :loading="retryingWorktreeCleanup"
                      @click="retryCurrentWorktreeCleanup"
                    >
                      <q-tooltip>重试工作树清理</q-tooltip>
                    </q-btn>
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
                <q-item v-if="latestTokenUsage">
                  <q-item-section>
                    <q-item-label caption>Token 用量</q-item-label>
                    <q-item-label class="token-usage-summary">
                      <span>输入 {{ formatTokenCount(latestTokenUsage.inputTokens) }}</span>
                      <span>缓存 {{ formatTokenCount(latestTokenUsage.cachedInputTokens) }}</span>
                      <span>输出 {{ formatTokenCount(latestTokenUsage.outputTokens) }}</span>
                      <span
                        >推理 {{ formatTokenCount(latestTokenUsage.reasoningOutputTokens) }}</span
                      >
                      <span>累计 {{ formatTokenCount(latestTokenUsage.totalTokens) }}</span>
                      <span v-if="latestTokenUsage.contextWindow">
                        上下文 {{ formatTokenCount(latestTokenUsage.contextWindow) }}
                      </span>
                    </q-item-label>
                  </q-item-section>
                </q-item>
              </q-list>

              <q-btn
                class="full-width q-mt-md app-command-btn"
                outline
                color="negative"
                icon="close"
                label="关闭卡片"
                no-caps
                :loading="closing"
                :disable="!canClose || isClosed || loading || closing"
                @click="closeCurrentSession"
              />

              <q-separator spaced />

              <div class="append-history">
                <div class="append-history__title">追加描述</div>
                <q-list v-if="session?.promptAppends.length" bordered separator>
                  <q-item
                    v-for="item in session.promptAppends"
                    :key="item.id"
                    clickable
                    v-ripple
                    aria-label="编辑追加提示"
                    @click="openPromptAppendEditor(item)"
                  >
                    <q-item-section>
                      <q-item-label class="append-history__body">{{ item.body }}</q-item-label>
                      <div v-if="item.attachments.length" class="append-history__attachments">
                        <q-chip
                          v-for="attachment in item.attachments"
                          :key="attachment.id"
                          dense
                          square
                          outline
                          icon="attach_file"
                          color="primary"
                          text-color="primary"
                          :label="attachment.filename"
                        />
                      </div>
                      <q-item-label caption>{{ item.time }}</q-item-label>
                    </q-item-section>
                    <q-item-section side>
                      <q-icon name="edit" color="grey-7" />
                    </q-item-section>
                    <q-tooltip>编辑追加提示</q-tooltip>
                  </q-item>
                </q-list>
                <div v-else class="append-history__empty">暂无追加描述</div>
              </div>
            </q-tab-panel>

            <q-tab-panel name="changes" class="detail-diff-panel">
              <q-btn
                class="full-width q-mb-md app-command-btn"
                outline
                color="primary"
                icon="open_in_new"
                label="完整 Diff"
                no-caps
                :to="allDiffRoute"
              />
              <DiffWorkspace v-model="detailDiffWorkspaceState" :target="detailDiffTarget" />
            </q-tab-panel>

            <q-tab-panel name="artifacts">
              <SessionArtifactsPanel :session-id="sessionId" :refresh-key="artifactRefreshKey" />
            </q-tab-panel>
          </q-tab-panels>
        </q-card>
      </aside>
    </div>

    <q-dialog
      v-model="promptEditDialogOpen"
      :maximized="$q.screen.lt.sm"
      :persistent="promptEditSaving"
    >
      <q-card class="prompt-edit-dialog app-content-dialog" aria-label="编辑追加提示">
        <q-card-section class="prompt-edit-dialog__header">
          <div class="text-subtitle1 text-weight-bold">编辑追加提示</div>
          <q-btn
            v-close-popup
            flat
            round
            dense
            class="app-icon-btn"
            icon="close"
            aria-label="关闭"
            :disable="promptEditSaving"
          >
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </q-card-section>
        <q-separator />
        <q-card-section class="prompt-edit-dialog__body">
          <q-banner v-if="promptEditError" dense class="prompt-edit-dialog__error">
            <template #avatar>
              <q-icon name="error_outline" />
            </template>
            {{ promptEditError }}
          </q-banner>
          <q-input
            v-model="promptEditBody"
            outlined
            type="textarea"
            autogrow
            label="追加提示正文"
            :disable="promptEditSaving"
          />
          <div v-if="promptEditTarget?.attachments.length" class="prompt-edit-dialog__attachments">
            <div class="text-caption text-muted">附件保持不变</div>
            <div class="append-history__attachments">
              <q-chip
                v-for="attachment in promptEditTarget.attachments"
                :key="attachment.id"
                dense
                square
                outline
                icon="attach_file"
                color="primary"
                text-color="primary"
                :label="attachment.filename"
              />
            </div>
          </div>
        </q-card-section>
        <q-separator />
        <q-card-actions align="right">
          <q-btn v-close-popup flat no-caps icon="close" label="取消" :disable="promptEditSaving" />
          <q-btn
            unelevated
            no-caps
            color="primary"
            icon="save"
            label="保存"
            :loading="promptEditSaving"
            :disable="!canSavePromptAppendEdit"
            @click="savePromptAppendEdit"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import { Notify } from 'quasar';
import { useRoute } from 'vue-router';

import AnswerUserPanel from '@/components/AnswerUserPanel.vue';
import CodexPromptComposer from '@/components/CodexPromptComposer.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';
import SessionEventMessage from '@/components/SessionEventMessage.vue';
import SessionArtifactsPanel from '@/components/SessionArtifactsPanel.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import WorkflowResultReview from '@/components/WorkflowResultReview.vue';
import { normalizePermissionMode } from '@/components/promptOptions';
import { useSessionDetail } from '@/composables/useSessionDetail';
import { deleteStagedAttachment, stageAttachment } from '@/services/attachments';
import { AnyCodeGraphQLError } from '@/services/graphqlClient';
import type { DiffWorkspaceState, DiffWorkspaceTarget } from '@/services/diff';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import { formatTokenCount } from '@/services/sessionTimelinePresentation';
import { reduceTranscriptEvents } from '@/services/sessionTimelineReducer';
import type { PromptAppend, QuestionAnswerInput, SessionMode } from '@/services/sessions';
import type { TranscriptItem } from '@/services/sessionTimeline';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';

const emit = defineEmits<{
  'session-title': [title: string];
}>();
const route = useRoute();
const sessionId = String(route.params.id ?? '');
const appendText = ref('');
const streamBodyRef = ref<HTMLElement | null>(null);
const appendFiles = ref<File[]>([]);
const appendUploading = ref(false);
const promptEditDialogOpen = ref(false);
const promptEditTarget = ref<PromptAppend | null>(null);
const promptEditBody = ref('');
const promptEditSaving = ref(false);
const promptEditError = ref('');
const composerModel = ref('');
const composerEffort = ref('');
const composerPermission = ref(normalizePermissionMode('workspace-write'));
const composerFast = ref(false);
const composerConfigReady = ref(false);
const detailView = ref<'session' | 'info' | 'changes' | 'artifacts'>('session');
// GLUE: mobile detail navigation adds the session view to the desktop info/changes tabs.
// Remove this mapping when desktop adopts the same three-view navigation.
const rightPanelTab = computed<'info' | 'changes' | 'artifacts'>({
  get: () => (detailView.value === 'session' ? 'info' : detailView.value),
  set: (value) => {
    detailView.value = value;
  },
});
const detailDiffTarget: DiffWorkspaceTarget = { kind: 'session', sessionId };
const detailDiffWorkspaceState = ref<DiffWorkspaceState>({
  mode: 'all',
  filePath: '',
  page: 1,
  pageSize: 20,
});
let mounted = false;
let preservingOlderEventScroll = false;
const {
  session,
  events,
  tokenUsage,
  eventsPageInfo,
  pendingQuestionBatches,
  loading,
  loadingOlderEvents,
  appending,
  executing,
  stopping,
  closing,
  retryingWorktreeCleanup,
  updatingConfig,
  questionsLoading,
  questionsSubmitting,
  approvalSubmitting,
  error: detailError,
  loadSessionDetail,
  appendDescription,
  updatePromptAppendBody,
  executeSession,
  stopSession,
  closeSession: closeSessionRequest,
  retryWorktreeCleanup,
  updateConfig,
  loadPendingQuestions,
  loadOlderEvents,
  submitPendingAnswers,
  submitApproval,
  startLiveUpdates,
  stopLiveUpdates,
} = useSessionDetail(sessionId);

// GLUE: Persisted prompt text disambiguates user-authored copies of injected guidance.
// Remove this projection when timeline messages expose their user/injected provenance.
const knownUserPrompts = computed(() => {
  const current = session.value;
  if (!current) return [];
  return [current.summary, ...current.promptAppends.map((prompt) => prompt.body)]
    .map((prompt) => prompt.trim())
    .filter(Boolean);
});
const canExecute = computed(() => session.value?.availableActions.includes('execute') ?? false);
const canClose = computed(() => session.value?.availableActions.includes('close') ?? false);
const worktreeCleanup = computed(() => session.value?.worktreeCleanup ?? null);
const canRetryWorktreeCleanup = computed(
  () => session.value?.availableActions.includes('retry_worktree_cleanup') ?? false,
);
const canCancelQueue = computed(
  () => session.value?.status === 'queued' && session.value.availableActions.includes('stop'),
);
const isClosed = computed(() => session.value?.status === 'closed');
const isWaitingForAnswer = computed(
  () =>
    !isClosed.value && (session.value?.pendingQuestion || session.value?.status === 'waiting_user'),
);
const isWaitingForApproval = computed(
  () => !isClosed.value && session.value?.status === 'waiting_approval',
);
const approvalPanelKey = computed(() => {
  const approval = session.value?.pendingApproval;
  if (!approval) return 'missing';
  return `${approval.workflowRunId}:${approval.nodeId}:${approval.nodeRunId}`;
});
const composerConfigDirty = computed(() => {
  const current = session.value;
  if (!current) return false;
  return (
    current.config.codexModel !== composerModel.value ||
    current.config.reasoningEffort !== composerEffort.value ||
    current.config.permissionMode !== composerPermission.value ||
    current.config.fastMode !== composerFast.value
  );
});

watch(
  () => session.value?.title,
  (title) => emit('session-title', title ?? ''),
  { immediate: true },
);
const canSavePromptAppendEdit = computed(() => {
  const target = promptEditTarget.value;
  const body = promptEditBody.value.trim();
  return Boolean(target && body && body !== target.body.trim() && !promptEditSaving.value);
});
const streamEntries = computed<TranscriptItem[]>(() => reduceTranscriptEvents(events.value));
const artifactRefreshKey = computed(() => {
  for (let index = streamEntries.value.length - 1; index >= 0; index--) {
    const entry = streamEntries.value[index];
    if (
      entry?.content.__typename === 'TranscriptUnknownContent' &&
      entry.content.rawType.startsWith('artifact.')
    ) {
      return entry.id;
    }
  }
  return '';
});
const latestTokenUsage = computed(() => tokenUsage.value);
const composerAction = computed(() => {
  const current = session.value;
  if (!current) return null;
  if (current.status === 'closed') return null;
  if (appendText.value.trim().length > 0 || appendFiles.value.length > 0) {
    return {
      icon: 'send',
      color: 'primary',
      tooltip: '发送追加描述',
      loading: appendUploading.value || appending.value || updatingConfig.value,
      disabled: appendUploading.value || stopping.value || updatingConfig.value,
      run: sendAppend,
    };
  }
  if (current.status === 'starting' || current.status === 'running') {
    return {
      icon: 'stop',
      color: 'negative',
      tooltip: '运行中，点击停止',
      loading: stopping.value,
      disabled: appending.value || executing.value,
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
  if (canExecute.value) {
    return {
      icon: 'play_arrow',
      color: 'positive',
      tooltip: composerConfigDirty.value ? '应用配置并运行' : '运行会话',
      loading: executing.value || updatingConfig.value,
      disabled: appending.value || executing.value || stopping.value || updatingConfig.value,
      run: executeWithComposerConfig,
    };
  }
  if (composerConfigDirty.value) {
    return {
      icon: 'save',
      color: 'primary',
      tooltip: '应用模型和思考强度',
      loading: updatingConfig.value,
      disabled: appending.value || executing.value || stopping.value,
      run: saveComposerConfig,
    };
  }
  return {
    icon: 'pause_circle',
    color: 'grey-7',
    tooltip: '已暂停',
    loading: false,
    disabled: true,
    run: executeSession,
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

function closeReasonLabel(value: string) {
  const labels: Record<string, string> = {
    user_closed: '用户关闭',
    merged_closed: '合并后关闭',
    workflow_closed: '流程关闭',
  };
  return labels[value] ?? value;
}

function worktreeCleanupLabel(status: string) {
  const labels: Record<string, string> = {
    provisioning: '创建中',
    active: '使用中',
    pending: '等待清理',
    failed: '清理失败',
    cleaned: '已清理',
  };
  return labels[status] ?? status;
}

function worktreeCleanupColor(status: string) {
  if (status === 'failed') return 'negative';
  if (status === 'pending' || status === 'provisioning') return 'warning';
  if (status === 'cleaned') return 'positive';
  return 'primary';
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

function notifyAppendError(err: unknown, fallback: string, cleanupError = '') {
  if (wasNotified(err) && !cleanupError) return;
  Notify.create({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message: cleanupError
      ? `${fallback}：${errorMessage(err)}；${cleanupError}`
      : `${fallback}：${errorMessage(err)}`,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
}

async function cleanupStagedAttachments(ids: string[]) {
  if (ids.length === 0) return '';
  const results = await Promise.allSettled(
    ids.map((id) => deleteStagedAttachment(id, { notify: false })),
  );
  const failed = results.find(
    (result) => result.status === 'rejected' && !isStagedAttachmentAlreadyGone(result.reason),
  );
  if (!failed || failed.status !== 'rejected') return '';
  return `已上传附件清理失败：${errorMessage(failed.reason)}`;
}

function isStagedAttachmentAlreadyGone(err: unknown) {
  return err instanceof AnyCodeGraphQLError && err.code === 'not_found';
}

function errorMessage(err: unknown) {
  return err instanceof Error ? err.message : String(err);
}

function wasNotified(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
}

function openPromptAppendEditor(prompt: PromptAppend) {
  promptEditTarget.value = prompt;
  promptEditBody.value = prompt.body;
  promptEditError.value = '';
  promptEditDialogOpen.value = true;
}

async function savePromptAppendEdit() {
  const target = promptEditTarget.value;
  if (!target || !canSavePromptAppendEdit.value) return;
  promptEditSaving.value = true;
  promptEditError.value = '';
  try {
    await updatePromptAppendBody(target.id, promptEditBody.value);
    promptEditDialogOpen.value = false;
    promptEditTarget.value = null;
    promptEditBody.value = '';
  } catch (err) {
    promptEditError.value = err instanceof Error ? err.message : '保存追加提示失败';
  } finally {
    promptEditSaving.value = false;
  }
}

async function sendAppend() {
  if (isClosed.value || appendUploading.value || appending.value) return;
  const text = appendText.value;
  const selectedFiles = [...appendFiles.value];
  const stagedAttachmentIds: string[] = [];
  let phase: 'upload' | 'append' = selectedFiles.length > 0 ? 'upload' : 'append';
  appendUploading.value = selectedFiles.length > 0;
  try {
    await saveComposerConfig();
    for (const file of selectedFiles) {
      const attachment = await stageAttachment(file);
      stagedAttachmentIds.push(attachment.id);
    }
    appendUploading.value = false;
    phase = 'append';
    await appendDescription(text, stagedAttachmentIds);
    appendText.value = '';
    appendFiles.value = [];
  } catch (err) {
    appendUploading.value = false;
    const cleanupError = await cleanupStagedAttachments(stagedAttachmentIds);
    notifyAppendError(err, phase === 'upload' ? '附件上传失败' : '追加描述失败', cleanupError);
  } finally {
    appendUploading.value = false;
  }
}

async function executeWithComposerConfig() {
  if (!canExecute.value) return;
  await saveComposerConfig();
  await executeSession();
}

async function saveComposerConfig() {
  const current = session.value;
  if (!current || !composerConfigDirty.value) return;
  const config = {
    codexModel: composerModel.value,
    reasoningEffort: composerEffort.value,
    permissionMode: composerPermission.value,
    fastMode: composerFast.value,
  };
  await updateConfig(config);
}

async function closeCurrentSession() {
  if (!canClose.value || isClosed.value || closing.value) return;
  try {
    await closeSessionRequest();
  } catch (err) {
    notifyError(err, '关闭卡片失败');
  }
}

async function retryCurrentWorktreeCleanup() {
  if (!canRetryWorktreeCleanup.value) return;
  try {
    await retryWorktreeCleanup();
  } catch (err) {
    notifyError(err, '重试工作树清理失败');
  }
}

async function submitAnswers(batchId: string, answers: QuestionAnswerInput[]) {
  await submitPendingAnswers(batchId, answers);
}

function isEventStreamAtBottom(body: HTMLElement) {
  return body.scrollHeight - body.scrollTop - body.clientHeight <= 1;
}

watch(
  () => events.value.length,
  () => {
    if (loadingOlderEvents.value || preservingOlderEventScroll) return;
    const body = streamBodyRef.value;
    if (body && isEventStreamAtBottom(body)) {
      void scrollEventsToBottom();
    }
  },
  { flush: 'pre' },
);

watch(
  session,
  (value) => {
    if (!value) return;
    if (composerConfigReady.value && composerConfigDirty.value) return;
    composerModel.value = value.config.codexModel;
    composerEffort.value = value.config.reasoningEffort;
    composerPermission.value = normalizePermissionMode(value.config.permissionMode);
    composerFast.value = value.config.fastMode;
    composerConfigReady.value = true;
  },
  { immediate: true },
);

onMounted(() => {
  mounted = true;
  void initializeSessionDetail();
});

onUnmounted(() => {
  mounted = false;
  stopLiveUpdates();
});

async function initializeSessionDetail() {
  await startLiveUpdates();
  if (!mounted) return;
  await Promise.all([loadSessionDetail(), loadPendingQuestions()]);
  if (!mounted) return;
  await scrollEventsToBottom();
}

async function onEventScroll() {
  const body = streamBodyRef.value;
  if (!body || body.scrollTop > 64 || loadingOlderEvents.value || preservingOlderEventScroll)
    return;
  const previousHeight = body.scrollHeight;
  const anchor = captureEventScrollAnchor(body);
  preservingOlderEventScroll = true;
  try {
    while (mounted && body.scrollHeight <= previousHeight) {
      const requestedCursor = eventsPageInfo.value.nextCursor;
      if (!requestedCursor) break;
      await loadOlderEvents();
      await nextTick();
      if (!eventsPageInfo.value.nextCursor || eventsPageInfo.value.nextCursor === requestedCursor)
        break;
    }
    if (!restoreEventScrollAnchor(body, anchor)) {
      body.scrollTop = body.scrollHeight - previousHeight + body.scrollTop;
    }
  } finally {
    preservingOlderEventScroll = false;
  }
}

interface EventScrollAnchor {
  id: string;
  offsetTop: number;
}

function captureEventScrollAnchor(body: HTMLElement): EventScrollAnchor | null {
  const bodyTop = body.getBoundingClientRect().top;
  const item = [...body.querySelectorAll<HTMLElement>('[data-timeline-id]')].find(
    (candidate) => candidate.getBoundingClientRect().bottom >= bodyTop,
  );
  const id = item?.dataset.timelineId;
  if (!item || !id) return null;
  return { id, offsetTop: item.getBoundingClientRect().top - bodyTop };
}

function restoreEventScrollAnchor(body: HTMLElement, anchor: EventScrollAnchor | null) {
  if (!anchor) return false;
  const item = [...body.querySelectorAll<HTMLElement>('[data-timeline-id]')].find(
    (candidate) => candidate.dataset.timelineId === anchor.id,
  );
  if (!item) return false;
  const currentOffset = item.getBoundingClientRect().top - body.getBoundingClientRect().top;
  body.scrollTop += currentOffset - anchor.offsetTop;
  return true;
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

.detail-mobile-tabs {
  min-height: 56px;
  flex: 0 0 auto;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
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

.detail-error-banner {
  border: 1px solid color-mix(in srgb, var(--q-negative) 38%, var(--ac-border));
  color: var(--q-negative);
  background: color-mix(in srgb, var(--q-negative) 8%, var(--ac-surface));
}

.event-list {
  display: grid;
  gap: 10px;
  min-width: 0;
}

.token-usage-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 12px;
  color: var(--ac-text);
  font-size: 12px;
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

.detail-approval-review {
  max-height: min(60vh, 640px);
  overflow: auto;
}

.detail-approval-review__result {
  padding: 16px;
  border-bottom: 1px solid var(--ac-border);
}

.detail-composer__primary-btn {
  border-radius: 11px;
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

.append-history__attachments {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin: 6px 0 2px;
}

.append-history__attachments :deep(.q-chip) {
  max-width: 100%;
}

.append-history__attachments :deep(.q-chip__content) {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.append-history__empty {
  color: var(--ac-text-muted);
  font-size: 13px;
}

.prompt-edit-dialog {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.prompt-edit-dialog__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.prompt-edit-dialog__body,
.prompt-edit-dialog__attachments {
  display: grid;
  gap: 12px;
}

.prompt-edit-dialog__body {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
}

.prompt-edit-dialog__error {
  color: var(--q-negative);
  background: var(--ac-surface-muted);
  border: 1px solid currentColor;
  border-radius: var(--ac-radius);
}

@media (max-width: 1023.98px) {
  .detail-page {
    height: 100%;
    overflow: hidden;
  }

  .detail-mobile-tabs {
    margin-bottom: 12px;
  }

  .detail-page .detail-grid {
    height: auto;
    min-height: 0;
    gap: 0;
    align-items: stretch;
  }

  .stream-card__body {
    padding: 0 10px 10px;
  }

  .event-panel {
    height: 100%;
    min-height: 0;
  }

  .event-panel--mobile-hidden,
  .right-panel--mobile-hidden {
    display: none;
  }

  .right-panel,
  .right-panel-card {
    height: 100%;
    min-height: 0;
  }
}
</style>
