<template>
  <q-page class="page-shell detail-page">
    <q-splitter
      class="detail-grid detail-splitter"
      reverse
      unit="px"
      :model-value="rightPanelWidth"
      :limits="[minRightPanelWidth, maxRightPanelWidth]"
      :disable="$q.screen.lt.md"
      :before-class="{ 'detail-splitter__panel--mobile-hidden': detailView !== 'session' }"
      :after-class="{ 'detail-splitter__panel--mobile-hidden': detailView === 'session' }"
      :separator-style="{ width: `${detailSplitterGap}px` }"
      @update:model-value="setPreferredRightPanelWidth"
    >
      <template #before>
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

        <div
          v-if="!isClosed"
          class="detail-composer"
          :class="{
            'detail-composer--collapsed':
              composerCollapsed && !isWaitingForAnswer && !isWaitingForApproval,
          }"
        >
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
              <q-badge rounded color="warning" class="app-on-warning" label="待回答" />
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
              v-if="!approvalLoading"
              :key="approvalPanelKey"
              :context-available="isPendingApprovalReviewable(session?.pendingApproval)"
              :submitting="approvalSubmitting"
              @submit="submitApproval"
            />
            <q-inner-loading :showing="approvalLoading">
              <q-spinner color="primary" size="32px" />
            </q-inner-loading>
          </div>
          <CodexPromptComposer
            v-else
            v-model:prompt="appendText"
            v-model:files="appendFiles"
            v-model:artifacts="appendArtifacts"
            v-model:model="composerModel"
            v-model:effort="composerEffort"
            v-model:permission="composerPermission"
            v-model:fast="composerFast"
            v-model:collapsed="composerCollapsed"
            compact
            collapsible
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
                :class="{ 'app-on-primary': composerAction.color === 'primary' }"
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
      </template>

      <template #separator>
        <div
          class="detail-splitter__handle"
          role="separator"
          tabindex="0"
          aria-label="调整左右面板宽度"
          aria-orientation="vertical"
          :aria-valuemin="minRightPanelWidth"
          :aria-valuemax="maxRightPanelWidth"
          :aria-valuenow="rightPanelWidth"
          :aria-valuetext="`右侧面板 ${rightPanelWidth} 像素`"
          @keydown.left.prevent="resizeRightPanel(splitterKeyboardStep)"
          @keydown.right.prevent="resizeRightPanel(-splitterKeyboardStep)"
        >
          <q-icon name="drag_indicator" size="18px" />
        </div>
      </template>

      <template #after>
        <aside class="right-panel">
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
            <q-tab name="artifacts" icon="inventory_2" label="临时文件" />
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
                <q-item v-if="session?.mode === 'workflow'">
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
                      <span
                        >本轮输入 {{ formatTokenCount(latestTokenUsage.currentInputTokens) }}</span
                      >
                      <span v-if="latestTokenUsage.contextWindow">
                        上下文占用 {{ contextUsagePercent(latestTokenUsage) }}
                      </span>
                      <span
                        >本轮缓存
                        {{ formatTokenCount(latestTokenUsage.currentCachedInputTokens) }}</span
                      >
                      <span>累计输入 {{ formatTokenCount(latestTokenUsage.inputTokens) }}</span>
                      <span
                        >累计缓存 {{ formatTokenCount(latestTokenUsage.cachedInputTokens) }}</span
                      >
                      <span>累计输出 {{ formatTokenCount(latestTokenUsage.outputTokens) }}</span>
                      <span v-if="latestTokenUsage.contextWindow">
                        窗口 {{ formatTokenCount(latestTokenUsage.contextWindow) }}
                      </span>
                      <span v-if="latestTokenUsage.compactionCount">
                        压缩 {{ latestTokenUsage.compactionCount }} 次
                      </span>
                    </q-item-label>
                    <q-item-label
                      v-if="session?.mode === 'workflow' && nodeUsage.length"
                      caption
                      class="token-usage-summary"
                    >
                      <span
                        v-for="item in nodeUsage"
                        :key="`${item.nodeRunId ?? ''}:${item.processRunId ?? ''}`"
                      >
                        节点 {{ item.nodeRunId }} {{ formatTokenCount(item.usage.totalTokens) }}
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
                      <div v-if="item.artifacts.length" class="append-history__attachments">
                        <q-chip
                          v-for="artifact in item.artifacts"
                          :key="artifact.id"
                          dense
                          square
                          outline
                          icon="link"
                          color="primary"
                          text-color="primary"
                          :label="artifact.logicalPath || artifact.filename"
                        />
                      </div>
                      <q-item-label caption>{{ item.time }}</q-item-label>
                    </q-item-section>
                    <q-item-section side>
                      <q-icon name="edit" class="text-muted" />
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
              <DiffWorkspace
                v-model="detailDiffWorkspaceState"
                :target="detailDiffTarget"
                :show-file-navigation="false"
                lazy-file-details
                :refresh-key="diffUpdateVersion"
              />
            </q-tab-panel>

            <q-tab-panel name="artifacts">
              <SessionArtifactsPanel
                :session-id="sessionId"
                :refresh-key="artifactRefreshKey"
                allow-reference
                @reference-artifact="referenceArtifact"
                @artifact-deleted="handleArtifactDeleted"
              />
            </q-tab-panel>
          </q-tab-panels>
        </q-card>
      </aside>
      </template>

      <q-resize-observer @resize="onDetailSplitterResize" />
    </q-splitter>

    <q-tabs v-model="detailView" class="detail-mobile-tabs lt-md" dense align="justify">
      <q-tab name="session" icon="forum" label="会话" />
      <q-tab name="info" icon="info" label="信息" />
      <q-tab name="changes" icon="difference" label="变更" />
      <q-tab name="artifacts" icon="inventory_2" label="临时文件" />
    </q-tabs>

    <q-dialog
      v-model="promptEditDialogOpen"
      :persistent="promptEditSaving"
    >
      <PromptAppendEditPanel
        v-model:body="promptEditBody"
        :target="promptEditTarget"
        :saving="promptEditSaving"
        :error="promptEditError"
        :can-save="canSavePromptAppendEdit"
        @cancel="promptEditDialogOpen = false"
        @save="savePromptAppendEdit"
      />
    </q-dialog>

    <q-dialog
      v-model="eventResourceDialogOpen"
      @hide="clearEventResource"
    >
      <q-card class="event-resource-dialog app-content-dialog" aria-label="事件文件">
        <q-card-section class="event-resource-dialog__header">
          <div class="event-resource-dialog__title">
            <q-icon :name="eventResourceKind === 'diff' ? 'difference' : fileIcon(eventResourceFile)" />
            <span>{{ eventResourceTitle }}</span>
          </div>
          <div class="row items-center q-gutter-xs">
            <q-btn
              v-if="eventResourceKind === 'file' && eventResourceFile"
              flat
              round
              dense
              icon="download"
              aria-label="下载文件"
              :loading="eventResourceDownloading"
              @click="downloadEventResource"
            >
              <q-tooltip>下载</q-tooltip>
            </q-btn>
            <q-btn v-close-popup flat round dense icon="close" aria-label="关闭">
              <q-tooltip>关闭</q-tooltip>
            </q-btn>
          </div>
        </q-card-section>
        <q-separator />
        <q-card-section class="event-resource-dialog__body">
          <DiffWorkspace
            v-if="eventResourceKind === 'diff'"
            v-model="eventDiffState"
            :target="detailDiffTarget"
            :show-file-navigation="false"
          />
          <SessionFilePreview v-else :file="eventResourceFile" />
        </q-card-section>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import { Notify, useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import AnswerUserPanel from '@/components/AnswerUserPanel.vue';
import CodexPromptComposer from '@/components/CodexPromptComposer.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';
import PromptAppendEditPanel from '@/components/PromptAppendEditPanel.vue';
import SessionEventMessage from '@/components/SessionEventMessage.vue';
import SessionArtifactsPanel from '@/components/SessionArtifactsPanel.vue';
import SessionFilePreview from '@/components/SessionFilePreview.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import WorkflowResultReview from '@/components/WorkflowResultReview.vue';
import { normalizePermissionMode } from '@/components/promptOptions';
import { useSessionDetail } from '@/composables/useSessionDetail';
import { deleteStagedAttachment, stageAttachment } from '@/services/attachments';
import { AnyCodeGraphQLError } from '@/services/graphqlClient';
import type { DiffWorkspaceState, DiffWorkspaceTarget } from '@/services/diff';
import { getSessionDiffFiles } from '@/services/diff';
import {
  matchChangedFilePath,
  parseSessionEventResourceReference,
  provideSessionEventResourceOpener,
} from '@/services/sessionEventResources';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import { formatTokenCount } from '@/services/sessionTimelinePresentation';
import { reduceTranscriptEvents } from '@/services/sessionTimelineReducer';
import {
  downloadSessionFile,
  listSessionFiles,
  resolveSessionArtifacts,
  type SessionFile,
} from '@/services/sessionFiles';
import type { PromptAppend, QuestionAnswerInput, SessionMode } from '@/services/sessions';
import type { TranscriptItem } from '@/services/sessionTimeline';
import type { TranscriptTokenUsage } from '@/services/sessionTimeline';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';

const emit = defineEmits<{
  'session-title': [title: string];
}>();
const $q = useQuasar();
const route = useRoute();
const router = useRouter();
const sessionId = String(route.params.id ?? '');
const defaultRightPanelWidth = 360;
const minRightPanelWidth = 320;
const minLeftPanelWidth = 480;
const detailSplitterGap = 16;
const splitterKeyboardStep = 16;
const detailSplitterStorageKey = 'anycode:session-detail:right-panel-width';
const initialDetailSplitterWidth = Math.max(
  minLeftPanelWidth + minRightPanelWidth + detailSplitterGap,
  window.innerWidth - 48,
);
const preferredRightPanelWidth = ref(readPreferredRightPanelWidth());
const detailSplitterWidth = ref(initialDetailSplitterWidth);
const maxRightPanelWidth = computed(() =>
  Math.max(minRightPanelWidth, detailSplitterWidth.value - minLeftPanelWidth - detailSplitterGap),
);
const rightPanelWidth = computed(() =>
  Math.max(minRightPanelWidth, Math.min(preferredRightPanelWidth.value, maxRightPanelWidth.value)),
);
const appendText = ref('');
const streamBodyRef = ref<HTMLElement | null>(null);
const appendFiles = ref<File[]>([]);
const appendArtifacts = ref<SessionFile[]>([]);
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
const composerCollapsed = ref(true);
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
});
const eventDiffState = ref<DiffWorkspaceState>({ mode: 'single', filePath: '' });
const eventResourceDialogOpen = ref(false);
const eventResourceKind = ref<'diff' | 'file'>('file');
const eventResourceFile = ref<SessionFile | null>(null);
const eventResourceDownloading = ref(false);
let eventResourceRequest = 0;
let mounted = false;
let preservingOlderEventScroll = false;
let previousEventScrollTop = Number.POSITIVE_INFINITY;

function readPreferredRightPanelWidth() {
  try {
    const raw = window.localStorage.getItem(detailSplitterStorageKey);
    if (raw === null || raw.trim() === '') return defaultRightPanelWidth;
    const value = Number(raw);
    return Number.isFinite(value)
      ? Math.max(minRightPanelWidth, Math.round(value))
      : defaultRightPanelWidth;
  } catch {
    return defaultRightPanelWidth;
  }
}

function setPreferredRightPanelWidth(value: number) {
  if ($q.screen.lt.md || !Number.isFinite(value)) return;
  preferredRightPanelWidth.value = Math.round(
    Math.max(minRightPanelWidth, Math.min(value, maxRightPanelWidth.value)),
  );
  try {
    window.localStorage.setItem(detailSplitterStorageKey, String(preferredRightPanelWidth.value));
  } catch {
    // The active layout remains usable when browser storage is unavailable.
  }
}

function resizeRightPanel(delta: number) {
  setPreferredRightPanelWidth(rightPanelWidth.value + delta);
}

function onDetailSplitterResize(size: { width: number }) {
  detailSplitterWidth.value = size.width;
}

const {
  session,
  events,
  tokenUsage,
  nodeUsage,
  eventsPageInfo,
  pendingQuestionBatches,
  artifactUpdateVersion,
  diffUpdateVersion,
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
  approvalLoading,
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

provideSessionEventResourceOpener(openSessionEventResource);

function openSessionEventResource(reference: string, label = '') {
  const parsed = parseSessionEventResourceReference(reference, sessionId);
  if (!parsed) return false;
  void resolveSessionEventResource(parsed, label);
  return true;
}

async function resolveSessionEventResource(
  reference: NonNullable<ReturnType<typeof parseSessionEventResourceReference>>,
  label: string,
) {
  const request = ++eventResourceRequest;
  try {
    if (reference.kind === 'session-file') {
      const files = await listSessionFiles({ sessionId });
      if (request !== eventResourceRequest) return;
      const file = files.find((item) => item.id === reference.fileId);
      if (file) return focusEventArtifact(file);
      throw new Error('文件已不存在');
    }
    if (reference.kind === 'artifact') {
      const resolved = await resolveSessionArtifacts(sessionId, [reference.logicalPath]);
      if (request !== eventResourceRequest) return;
      const file = resolved[0]?.file;
      if (file) return focusEventArtifact(file);
      throw new Error('临时文件已不存在');
    }

    const [diffResult, artifactResult] = await Promise.allSettled([
      getSessionDiffFiles({ sessionId }),
      reference.path.startsWith('/')
        ? Promise.resolve([])
        : resolveSessionArtifacts(sessionId, [reference.path]),
    ]);
    if (request !== eventResourceRequest) return;
    if (diffResult.status === 'fulfilled') {
      const filePath = matchChangedFilePath(
        reference.path,
        diffResult.value.files.map((file) => file.path),
      );
      if (filePath) return openEventDiff(filePath);
    }
    if (artifactResult.status === 'fulfilled' && artifactResult.value[0]?.file) {
      return focusEventArtifact(artifactResult.value[0].file);
    }
    throw new Error(label ? `无法查看“${label}”` : '无法查看此文件');
  } catch (err) {
    if (request !== eventResourceRequest) return;
    Notify.create({ type: 'negative', message: errorMessage(err) || '读取文件失败' });
  }
}

function openEventDiff(filePath: string) {
  if ($q.screen.lt.sm) {
    void router.push({
      path: '/diff',
      query: { sessionId, mode: 'single', filePath },
    });
    return;
  }
  eventDiffState.value = { mode: 'single', filePath };
  eventResourceFile.value = null;
  eventResourceKind.value = 'diff';
  eventResourceDialogOpen.value = true;
}

function focusEventArtifact(file: SessionFile) {
  if ($q.screen.lt.sm) {
    void router.push({
      name: 'session-artifact',
      params: { id: sessionId, fileId: file.id },
    });
    return;
  }
  eventResourceFile.value = file;
  eventResourceKind.value = 'file';
  eventResourceDialogOpen.value = true;
}

const eventResourceTitle = computed(() =>
  eventResourceKind.value === 'diff'
    ? eventDiffState.value.filePath
    : eventResourceFile.value?.logicalPath || eventResourceFile.value?.filename || '文件预览',
);

async function downloadEventResource() {
  const file = eventResourceFile.value;
  if (!file) return;
  eventResourceDownloading.value = true;
  try {
    await downloadSessionFile(file);
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err) || '下载文件失败' });
  } finally {
    eventResourceDownloading.value = false;
  }
}

function fileIcon(file: SessionFile | null) {
  const icons: Record<string, string> = {
    image: 'image',
    pdf: 'picture_as_pdf',
    video: 'movie',
    audio: 'audio_file',
    archive: 'folder_zip',
    text: 'description',
  };
  return file ? (icons[file.artifactKind] ?? 'draft') : 'draft';
}

function clearEventResource() {
  eventResourceRequest++;
  eventResourceFile.value = null;
}

// GLUE: Persisted prompt text disambiguates user-authored copies of injected guidance.
// Remove this fallback when timeline messages expose their user/injected provenance.
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
  () => !isClosed.value && session.value?.status === 'waiting_user',
);
const isWaitingForApproval = computed(
  () => !isClosed.value && session.value?.status === 'waiting_approval',
);
const approvalPanelKey = computed(() => {
  const approval = session.value?.pendingApproval;
  if (!approval) return 'missing';
  return `${approval.sessionId}:${approval.nodeId}:${approval.nodeRunId ?? ''}`;
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
const artifactRefreshKey = computed(() => String(artifactUpdateVersion.value));
const latestTokenUsage = computed(() => tokenUsage.value);
const contextUsagePercent = (usage: TranscriptTokenUsage) => {
  if (!usage.contextWindow) return '-';
  return `${Math.min(100, Math.round((usage.currentInputTokens / usage.contextWindow) * 100))}%`;
};
const composerAction = computed(() => {
  const current = session.value;
  if (!current) return null;
  if (current.status === 'closed') return null;
  if (
    appendText.value.trim().length > 0 ||
    appendFiles.value.length > 0 ||
    appendArtifacts.value.length > 0
  ) {
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
      color: 'primary',
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
    color: 'primary',
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
  if ($q.screen.lt.sm) {
    void router.push({
      name: 'prompt-append-edit',
      params: { id: sessionId, promptId: prompt.id },
    });
    return;
  }
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
  const selectedArtifacts = [...appendArtifacts.value];
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
    // GLUE: uploads become staged IDs while directory-backed artifacts already have session file IDs.
    await appendDescription(
      text,
      stagedAttachmentIds,
      selectedArtifacts.map((artifact) => artifact.id),
    );
    appendText.value = '';
    appendFiles.value = [];
    appendArtifacts.value = [];
    composerCollapsed.value = true;
  } catch (err) {
    appendUploading.value = false;
    const cleanupError = await cleanupStagedAttachments(stagedAttachmentIds);
    notifyAppendError(err, phase === 'upload' ? '附件上传失败' : '追加描述失败', cleanupError);
  } finally {
    appendUploading.value = false;
  }
}

function referenceArtifact(artifact: SessionFile) {
  if (!appendArtifacts.value.some((item) => item.id === artifact.id)) {
    appendArtifacts.value = [...appendArtifacts.value, artifact];
  }
  if ($q.screen.lt.md) detailView.value = 'session';
}

function handleArtifactDeleted(artifact: SessionFile) {
  appendArtifacts.value = appendArtifacts.value.filter((item) => item.id !== artifact.id);
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
  await Promise.all([loadSessionDetail(), loadPendingQuestions()]);
  if (!mounted) return;
  startLiveUpdates();
  await scrollEventsToBottom();
}

async function onEventScroll() {
  const body = streamBodyRef.value;
  if (!body) return;
  const currentScrollTop = body.scrollTop;
  const scrollingUp = currentScrollTop < previousEventScrollTop;
  previousEventScrollTop = currentScrollTop;
  if (!scrollingUp || currentScrollTop > 64 || loadingOlderEvents.value || preservingOlderEventScroll)
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
    previousEventScrollTop = body.scrollTop;
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
  previousEventScrollTop = body.scrollTop;
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
  border: 0;
  border-radius: 0;
  background: var(--ac-surface);
}

.detail-page .detail-grid {
  width: 100%;
  height: 100%;
  flex: 1 1 auto;
  min-height: 0;
}

.detail-splitter > :deep(.q-splitter__panel) {
  min-width: 0;
  overflow: hidden;
}

.detail-splitter > :deep(.q-splitter__separator) {
  background: transparent;
}

.detail-splitter > :deep(.q-splitter__separator::before) {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 1px;
  content: '';
  background: var(--ac-border);
  transform: translateX(-50%);
}

.detail-splitter__handle {
  display: flex;
  width: 12px;
  height: 48px;
  align-items: center;
  justify-content: center;
  color: var(--ac-text-muted);
  background: var(--ac-surface-raised);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
  outline: 0;
  cursor: col-resize;
  transition:
    color 120ms ease,
    border-color 120ms ease,
    background-color 120ms ease,
    box-shadow 120ms ease;
}

.detail-splitter > :deep(.q-splitter__separator:hover) .detail-splitter__handle,
.detail-splitter.q-splitter--active .detail-splitter__handle {
  color: var(--q-primary);
  border-color: var(--q-primary);
  background: var(--ac-surface-muted);
}

.detail-splitter__handle:focus-visible {
  color: var(--q-primary);
  border-color: var(--q-primary);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--q-primary) 28%, transparent);
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

.event-resource-dialog {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.event-resource-dialog__header,
.event-resource-dialog__title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}

.event-resource-dialog__header {
  justify-content: space-between;
}

.event-resource-dialog__title span {
  min-width: 0;
  overflow-wrap: anywhere;
  word-break: break-word;
}

.event-resource-dialog__body {
  min-height: 0;
  flex: 1 1 auto;
  overflow: auto;
  padding: 12px;
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

.detail-diff-panel {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.detail-diff-panel :deep(.diff-workspace) {
  flex: 1 1 auto;
  min-height: 0;
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

.detail-composer--collapsed {
  min-height: 0;
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
  position: relative;
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
  grid-template-columns: minmax(0, 1fr);
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

.append-history :deep(.q-item__section--main) {
  flex-wrap: nowrap;
}

.append-history__attachments {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin: 6px 0 2px;
}

.append-history__attachments :deep(.q-chip) {
  margin: 0;
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
    padding: 0;
    overflow: hidden;
  }

  .detail-mobile-tabs {
    margin: 0;
  }

  .detail-page .detail-grid {
    display: block;
    width: 100%;
    height: 100%;
    min-height: 0;
  }

  .detail-splitter > :deep(.q-splitter__separator) {
    display: none;
  }

  .detail-splitter > :deep(.q-splitter__panel) {
    width: 100% !important;
    height: 100% !important;
  }

  .detail-splitter > :deep(.detail-splitter__panel--mobile-hidden) {
    display: none;
  }

  .stream-card__body {
    padding: 0 10px 10px;
  }

  .event-panel {
    height: 100%;
    min-height: 0;
  }

  .right-panel,
  .right-panel-card {
    height: 100%;
    min-height: 0;
  }

  .stream-card,
  .right-panel-card {
    border: 0;
    border-radius: 0;
  }
}
</style>
