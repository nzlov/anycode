<template>
  <q-page
    class="workbench-page page-shell"
    :class="{ 'workbench-page--desktop-focus': showDesktopFocusLayout }"
  >
    <div class="overview-filter-toolbar">
      <div
        v-if="projectChips.length"
        class="overview-project-filters"
        role="group"
        aria-label="项目卡片显示筛选"
      >
        <q-chip
          v-for="project in projectChips"
          :key="project.id"
          clickable
          :outline="!isProjectVisible(project.id)"
          :color="isProjectVisible(project.id) ? 'positive' : 'grey-4'"
          :text-color="isProjectVisible(project.id) ? 'dark' : 'grey-8'"
          :icon="isProjectVisible(project.id) ? 'visibility' : 'visibility_off'"
          :aria-pressed="isProjectVisible(project.id)"
          :aria-label="`${isProjectVisible(project.id) ? '隐藏' : '显示'} ${project.name} 项目卡片`"
          @click="toggleProjectVisibility(project.id)"
        >
          {{ project.name }}
        </q-chip>
      </div>
    </div>

    <section class="overview-card-section">
      <div v-if="visibleLatestCards.length > 0" class="overview-card-grid">
        <q-card
          v-for="card in visibleLatestCards"
          :key="card.id"
          flat
          bordered
          clickable
          tabindex="0"
          class="overview-session-card cursor-pointer"
          :class="overviewCardClass(card)"
          @click="openSessionCard(card.id)"
          @touchend="releaseCardContextMenuTouch(card.id)"
          @touchcancel="clearCardClickSuppression(card.id)"
          @keyup.enter.self="openSessionCard(card.id)"
          @keyup.space.self.prevent="openSessionCard(card.id)"
        >
          <q-card-section class="overview-session-card__body">
            <div class="overview-card-chips">
              <q-badge
                rounded
                class="lane-status-chip"
                :class="statusChipClass(card.status)"
                :label="statusLabel(card.status)"
              />
              <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
              <q-badge rounded class="lane-mode-chip" :label="priorityLabel(card.priority)" />
              <q-badge
                v-if="card.pendingQuestion"
                rounded
                color="warning"
                text-color="dark"
                label="待回答"
              />
            </div>

            <div class="overview-card-title">{{ card.title }}</div>

            <div class="overview-card-meta-grid">
              <div>
                <span class="overview-card-meta__label">所属项目</span>
                <span>{{ card.projectName }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">基础分支</span>
                <span>{{ card.branch }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">工作分支</span>
                <span>{{ card.worktreeBranch || '-' }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">当前节点</span>
                <span>{{ card.node }}</span>
              </div>
              <div>
                <span class="overview-card-meta__label">最近操作</span>
                <span>{{ card.updatedAt }}</span>
              </div>
            </div>

            <div class="overview-card-footer">
              <div class="overview-card-secondary-actions" @contextmenu.stop @touchstart.stop>
                <q-btn
                  v-if="card.todoList"
                  flat
                  dense
                  no-caps
                  class="overview-todo-btn app-command-btn"
                  icon="checklist"
                  :label="`${card.todoList.completed}/${card.todoList.total}`"
                  aria-label="查看 TODO List"
                  @click.stop
                  @contextmenu.stop
                  @touchstart.stop
                  @keyup.enter.stop
                  @keyup.space.stop
                >
                  <q-tooltip>TODO List</q-tooltip>
                  <q-menu
                    anchor="top left"
                    self="bottom left"
                    class="overview-todo-menu"
                    @click.stop
                  >
                    <q-list dense separator class="app-touch-list">
                      <q-item
                        v-for="(item, index) in card.todoList.items"
                        :key="`${card.id}-${index}`"
                      >
                        <q-item-section avatar>
                          <q-icon
                            :name="item.completed ? 'check_circle' : 'radio_button_unchecked'"
                            :color="item.completed ? 'positive' : 'grey-6'"
                          />
                        </q-item-section>
                        <q-item-section>
                          <q-item-label :class="{ 'overview-todo-item--done': item.completed }">
                            {{ item.text }}
                          </q-item-label>
                        </q-item-section>
                      </q-item>
                    </q-list>
                  </q-menu>
                </q-btn>

                <q-btn
                  v-if="card.filesChanged > 0"
                  flat
                  dense
                  no-caps
                  class="overview-diff-btn app-command-btn"
                  icon="difference"
                  :label="String(card.filesChanged)"
                  :aria-label="`查看 ${card.filesChanged} 个变更文件`"
                  @click.stop="openDiffDialog(card)"
                  @keyup.enter.stop
                  @keyup.space.stop
                >
                  <q-tooltip>查看 Diff</q-tooltip>
                </q-btn>
              </div>

              <div class="overview-card-actions" @contextmenu.stop @touchstart.stop>
                <q-btn
                  v-if="card.pendingQuestion"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn lane-icon-btn--warning"
                  icon="help"
                  aria-label="回答问题"
                  :loading="questionsLoading && activeQuestionSessionId === card.id"
                  @click.stop="openAnswerDialog(card.id)"
                >
                  <q-tooltip>回答待处理问题</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="card.status === 'waiting_approval'"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn lane-icon-btn--warning"
                  icon="fact_check"
                  aria-label="人工审核"
                  :loading="approvalLoading && approvalSessionId === card.id"
                  @click.stop="openApprovalDialog(card)"
                >
                  <q-tooltip>人工审核</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="card.status === 'queued' && card.availableActions.includes('stop')"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn"
                  color="negative"
                  icon="cancel"
                  aria-label="取消排队"
                  :loading="cardActionLoading && activeActionSessionId === card.id"
                  @click.stop="cancelQueuedCard(card)"
                >
                  <q-tooltip>取消排队</q-tooltip>
                </q-btn>
                <q-btn
                  v-if="cardAction(card)"
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn"
                  :color="cardAction(card)?.color"
                  :icon="cardAction(card)?.icon"
                  :aria-label="cardAction(card)?.tooltip"
                  :loading="cardActionLoading && activeActionSessionId === card.id"
                  :disable="cardAction(card)?.disabled"
                  @click.stop="runCardAction(card)"
                >
                  <q-tooltip>{{ cardAction(card)?.tooltip }}</q-tooltip>
                </q-btn>
              </div>
            </div>
          </q-card-section>
          <q-menu
            context-menu
            @before-show="handleCardContextMenuBeforeShow(card.id, $event)"
            @click.stop
          >
            <q-list dense class="overview-card-menu app-touch-list">
              <q-item
                v-close-popup
                clickable
                tag="a"
                :href="router.resolve({ name: 'session-detail', params: { id: card.id } }).href"
                target="_blank"
                rel="noopener noreferrer"
                @click.stop
              >
                <q-item-section avatar>
                  <q-icon name="open_in_new" />
                </q-item-section>
                <q-item-section>在新标签页中打开</q-item-section>
              </q-item>
              <q-separator />
              <q-item-label header>优先级</q-item-label>
              <q-item
                v-for="priority in priorities"
                :key="priority"
                v-close-popup
                clickable
                :active="card.priority === priority"
                :disable="card.status === 'closed'"
                @click.stop="setCardPriority(card, priority)"
              >
                <q-item-section>{{ priorityLabel(priority) }}</q-item-section>
                <q-item-section v-if="card.priority === priority" side>
                  <q-icon name="check" color="primary" />
                </q-item-section>
              </q-item>
              <q-separator />
              <q-item
                v-close-popup
                clickable
                class="text-negative"
                :disable="!card.availableActions.includes('close')"
                @click.stop="closeCard(card)"
              >
                <q-item-section avatar>
                  <q-icon name="close" />
                </q-item-section>
                <q-item-section>关闭卡片</q-item-section>
              </q-item>
            </q-list>
          </q-menu>
        </q-card>
      </div>
    </section>

    <q-banner
      v-if="latestCards.length > 0 && visibleLatestCards.length === 0"
      rounded
      class="empty-lane-banner q-mt-md"
    >
      当前没有显示的卡片
    </q-banner>
    <q-banner v-else-if="latestCards.length === 0" rounded class="empty-lane-banner q-mt-md">
      暂无卡片
    </q-banner>

    <AnswerUserDialog
      v-model="answerDialog"
      :batches="pendingQuestionBatches"
      :loading="questionsLoading"
      :submitting="questionsSubmitting"
      :diff-loading="answerDiffLoading"
      :diff-error="answerDiffError"
      :diff-available="answerDiffAvailable"
      :diffs="answerDiffs"
      :diff-total="answerDiffTotal"
      :full-diff-route="answerAllDiffRoute"
      @submit="submitAnswers"
    />

    <q-dialog v-model="approvalDialog" :maximized="$q.screen.lt.sm">
      <q-card class="forward-approval-dialog app-content-dialog">
        <div class="forward-approval-dialog__tabs">
          <q-tabs v-model="approvalTab" dense align="left" class="text-primary">
            <q-tab name="output" icon="fact_check" label="审核结果" />
            <q-tab name="diff" icon="difference" label="Diff" />
          </q-tabs>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭人工审核" />
        </div>
        <q-separator />
        <q-tab-panels v-model="approvalTab" animated class="forward-approval-dialog__panels">
          <q-tab-panel name="output" class="forward-approval-dialog__panel">
            <WorkflowResultReview
              :phase="approvalPending?.phase ?? null"
              :result="approvalPending?.result ?? null"
            />
          </q-tab-panel>
          <q-tab-panel name="diff" class="forward-approval-dialog__panel">
            <SessionDiffPreview
              :loading="approvalLoading"
              :error="approvalDiffError"
              :available="approvalDiffAvailable"
              :file-diffs="approvalDiffs"
              :total="approvalDiffTotal"
              :full-diff-route="approvalAllDiffRoute"
            />
          </q-tab-panel>
        </q-tab-panels>
        <q-separator />
        <WorkflowApprovalPanel
          v-if="approvalDialog"
          :context-available="Boolean(approvalContext) && isPendingApprovalReviewable(approvalPending)"
          :submitting="approvalSubmitting"
          @submit="submitApproval"
        />
      </q-card>
    </q-dialog>

    <q-dialog v-model="diffDialog" :maximized="$q.screen.lt.sm" @hide="handleDiffDialogClosed">
      <q-card class="overview-diff-dialog app-content-dialog">
        <div class="overview-diff-dialog__header">
          <div class="text-subtitle1 text-weight-bold">Diff</div>
          <div class="overview-diff-dialog__header-actions">
            <q-btn
              flat
              round
              dense
              icon="open_in_new"
              aria-label="打开完整 Diff 页面"
              :to="diffDialogAllDiffRoute"
              @click.stop
            >
              <q-tooltip>打开完整 Diff 页面</q-tooltip>
            </q-btn>
            <q-btn v-close-popup flat round dense icon="close" aria-label="关闭 Diff" />
          </div>
        </div>
        <q-separator />
        <q-card-section class="overview-diff-dialog__body">
          <DiffWorkspace
            v-if="diffDialog"
            v-model="diffDialogWorkspaceState"
            :target="diffDialogTarget"
          />
        </q-card-section>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';
import SessionDiffPreview from '@/components/SessionDiffPreview.vue';
import WorkflowResultReview from '@/components/WorkflowResultReview.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import { useProjects } from '@/composables/useProjects';
import { useSessionsPage } from '@/composables/useSessionsPage';
import {
  getSessionAllDiff,
  getSessionDiffSummaries,
  type DiffWorkspaceState,
  type DiffWorkspaceTarget,
  type FileDiff,
  type SessionDiffSummary,
} from '@/services/diff';
import {
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { createOverviewCardGroups } from '@/services/overviewCardGroups';
import {
  activeOverviewDiffSessionIds,
  createOverviewDiffSummaryController,
} from '@/services/overviewDiffSummary';
import { shouldReconnectCardStream } from '@/services/sessionEventTimeline';
import { sessionStatusLabel as statusLabel } from '@/services/sessionStatusPresentation';
import {
  closeSession,
  executeSession,
  getPendingQuestionBatches,
  stopSession,
  subscribeSessionCardChanged,
  submitQuestionBatch,
  submitWorkflowApproval,
  updateSessionPriority,
  type QuestionAnswerInput,
  type QuestionBatch,
  type PendingApproval,
  type SessionCard,
  type SessionMode,
  type SessionPriority,
  type SessionStatus,
} from '@/services/sessions';
import { isPendingApprovalReviewable } from '@/services/workflowApprovalReview';

const route = useRoute();
const router = useRouter();
const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const hiddenProjectStorageKey = 'anycode.overview.hidden-projects.v1';
const showDesktopFocusLayout = computed(() => $q.screen.width >= overviewDesktopMinWidth);
const projectScopeId = computed(() => {
  const value = route.query.projectId;
  return typeof value === 'string' ? value : '';
});

const {
  rows: latestRows,
  projectId: latestProjectId,
  loadSessions: loadLatestSessions,
} = useSessionsPage({
  projectId: projectScopeId.value,
  range: 'latest',
  page: 1,
  pageSize: 100,
  sort: 'updated_at desc',
  loadAll: true,
});
const { projects, loadProjects } = useProjects();

const diffSummariesBySessionId = ref<Record<string, SessionDiffSummary>>({});
const hiddenProjectIds = ref(readHiddenProjectIds());
const overviewCardGroups = computed(() => createOverviewCardGroups(latestRows.value, []));
const latestCards = computed(() => overviewCardGroups.value.latestCards.map(withDiffSummary));
const projectChips = computed(() => {
  const seen = new Set<string>();
  return latestCards.value
    .filter((card: SessionCard) => {
      if (seen.has(card.projectId)) return false;
      seen.add(card.projectId);
      return true;
    })
    .map((card: SessionCard) => ({
      id: card.projectId,
      name: card.projectName || card.projectId,
    }));
});
const visibleLatestCards = computed(() =>
  latestCards.value.filter((card: SessionCard) => !hiddenProjectIds.value.has(card.projectId)),
);
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);
const answerDiffLoading = ref(false);
const answerDiffs = ref<FileDiff[]>([]);
const answerDiffAvailable = ref(false);
const answerDiffTotal = ref(0);
const answerDiffError = ref('');
const approvalDialog = ref(false);
const approvalTab = ref<'output' | 'diff'>('output');
const approvalLoading = ref(false);
const approvalSubmitting = ref(false);
const approvalSessionId = ref('');
const approvalContext = ref<ApprovalContext | null>(null);
const approvalPending = ref<PendingApproval | null>(null);
let approvalContextGeneration = 0;
const approvalDiffs = ref<FileDiff[]>([]);
const approvalDiffAvailable = ref(false);
const approvalDiffTotal = ref(0);
const approvalDiffError = ref('');
const diffDialog = ref(false);
const diffDialogSessionId = ref('');
const diffDialogWorkspaceState = ref<DiffWorkspaceState>({
  mode: 'all',
  filePath: '',
  page: 1,
  pageSize: 20,
});
const cardActionLoading = ref(false);
const activeActionSessionId = ref('');
const activePrioritySessionId = ref('');
const activeCloseSessionId = ref('');
const priorities: SessionPriority[] = ['high', 'medium', 'low'];
const answerAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: activeQuestionSessionId.value, mode: 'all' },
}));
const approvalAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: approvalSessionId.value, mode: 'all' },
}));
const diffDialogAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: diffDialogSessionId.value, mode: 'all' },
}));
const diffDialogTarget = computed<DiffWorkspaceTarget>(() => ({
  kind: 'session',
  sessionId: diffDialogSessionId.value,
}));
let cardSubscription: ReturnType<typeof subscribeSessionCardChanged> | null = null;
let cardReconnectTimer: ReturnType<typeof setTimeout> | null = null;
let cardRefreshTimer: ReturnType<typeof setTimeout> | null = null;
let liveStopped = true;
// GLUE: suppress Quasar's synthetic post-long-press click; remove when QMenu consumes it upstream.
let suppressedCardClickId = '';
let cardClickSuppressionTimer: ReturnType<typeof setTimeout> | null = null;

interface ApprovalContext {
  workflowRunId: string;
  nodeId: string;
}

const diffSummaryController = createOverviewDiffSummaryController({
  loadSummaries: getSessionDiffSummaries,
  applySummaries: (sessionIds: string[], summaries: SessionDiffSummary[]) => {
    const next = { ...diffSummariesBySessionId.value };
    for (const sessionId of sessionIds) delete next[sessionId];
    for (const summary of summaries) next[summary.sessionId] = summary;
    diffSummariesBySessionId.value = next;
  },
  getVisibleCards: () => visibleLatestCards.value,
  isPageVisible: () => typeof document === 'undefined' || document.visibilityState === 'visible',
});

onMounted(() => {
  document.addEventListener('visibilitychange', handleVisibilityChange);
  void startOverview();
});

onUnmounted(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  clearCardClickSuppression();
  diffSummaryController.stop();
  stopOverviewLiveUpdates();
});

watch(projectScopeId, (value) => {
  latestProjectId.value = value;
  diffSummariesBySessionId.value = {};
  void loadOverviewSessions();
  if (!liveStopped) {
    startOverviewLiveUpdates();
  }
});

watch(
  () => projects.value.map((project) => project.id).join('\0'),
  () => pruneHiddenProjectIds(),
);

async function startOverview() {
  await loadProjects();
  pruneHiddenProjectIds();
  await loadOverviewSessions();
  diffSummaryController.start();
  startOverviewLiveUpdates();
}

async function loadOverviewSessions() {
  await loadLatestSessions();
  await diffSummaryController
    .refresh(visibleLatestCards.value.map((card: SessionCard) => card.id))
    .catch(() => undefined);
  diffSummaryController.syncPolling();
}

function withDiffSummary(card: SessionCard): SessionCard {
  const summary = diffSummariesBySessionId.value[card.id];
  return {
    ...card,
    filesChanged: summary?.state === 'changed' ? summary.filesChanged : 0,
  };
}

function readHiddenProjectIds() {
  if (typeof window === 'undefined') return new Set<string>();
  try {
    const stored = JSON.parse(window.localStorage.getItem(hiddenProjectStorageKey) ?? '[]');
    if (!Array.isArray(stored)) return new Set<string>();
    return new Set(stored.filter((id): id is string => typeof id === 'string' && id !== ''));
  } catch {
    return new Set<string>();
  }
}

function persistHiddenProjectIds() {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(
      hiddenProjectStorageKey,
      JSON.stringify([...hiddenProjectIds.value]),
    );
  } catch {
    // Browser storage can be unavailable; filtering still works for the current page.
  }
}

function pruneHiddenProjectIds() {
  const projectIds = new Set(projects.value.map((project) => project.id));
  const next = new Set(
    [...hiddenProjectIds.value].filter((projectId) => projectIds.has(projectId)),
  );
  if (next.size === hiddenProjectIds.value.size) return;
  hiddenProjectIds.value = next;
  persistHiddenProjectIds();
}

function isProjectVisible(projectId: string) {
  return !hiddenProjectIds.value.has(projectId);
}

function toggleProjectVisibility(projectId: string) {
  const next = new Set(hiddenProjectIds.value);
  if (next.has(projectId)) {
    next.delete(projectId);
  } else {
    next.add(projectId);
  }
  hiddenProjectIds.value = next;
  persistHiddenProjectIds();
  void diffSummaryController
    .refresh(visibleLatestCards.value.map((card: SessionCard) => card.id))
    .catch(() => undefined);
  diffSummaryController.syncPolling();
}

function handleVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void diffSummaryController
      .refresh(activeOverviewDiffSessionIds(visibleLatestCards.value))
      .catch(() => undefined);
  }
  diffSummaryController.syncPolling();
}

function startOverviewLiveUpdates(onSubscribed?: () => void) {
  liveStopped = false;
  cardSubscription?.unsubscribe();
  cardSubscription = subscribeSessionCardChanged(
    projectScopeId.value ? { projectId: projectScopeId.value } : {},
    {
      onData: scheduleOverviewRefresh,
      onError: scheduleOverviewReconnect,
      onClose: (close) => {
        void handleOverviewSubscriptionClose(close);
      },
      onSubscribed: onSubscribed ?? refreshOverviewAfterSubscriptionReady,
    },
  );
}

async function handleOverviewSubscriptionClose(close: GraphQLSubscriptionClose) {
  const reconnect = await shouldReconnectCardStream(close, () =>
    verifyGraphQLAccessKey(getGraphQLAccessKey()),
  );
  if (liveStopped) return;
  if (reconnect) {
    scheduleOverviewReconnect();
    return;
  }
  if (cardReconnectTimer) {
    clearTimeout(cardReconnectTimer);
    cardReconnectTimer = null;
  }
}

function stopOverviewLiveUpdates() {
  liveStopped = true;
  cardSubscription?.unsubscribe();
  cardSubscription = null;
  if (cardReconnectTimer) {
    clearTimeout(cardReconnectTimer);
    cardReconnectTimer = null;
  }
  if (cardRefreshTimer) {
    clearTimeout(cardRefreshTimer);
    cardRefreshTimer = null;
  }
}

function scheduleOverviewRefresh() {
  if (cardRefreshTimer) return;
  cardRefreshTimer = setTimeout(() => {
    cardRefreshTimer = null;
    void loadOverviewSessions();
  }, 300);
}

function scheduleOverviewReconnect() {
  if (liveStopped || cardReconnectTimer) return;
  cardReconnectTimer = setTimeout(() => {
    cardReconnectTimer = null;
    void reconnectOverviewLiveUpdates();
  }, 1500);
}

async function reconnectOverviewLiveUpdates() {
  if (liveStopped) return;
  await loadOverviewSessions();
  if (!liveStopped) {
    startOverviewLiveUpdates();
  }
}

function refreshOverviewAfterSubscriptionReady() {
  if (!liveStopped) void loadOverviewSessions();
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function priorityLabel(priority: SessionPriority) {
  const labels: Record<SessionPriority, string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
  };
  return labels[priority];
}

function statusChipClass(status: SessionStatus) {
  return `lane-status-chip--${status}`;
}

function overviewCardClass(card: SessionCard) {
  return `overview-session-card--${card.status}`;
}

function openSessionCard(sessionId: string) {
  if (suppressedCardClickId === sessionId) {
    clearCardClickSuppression(sessionId);
    return;
  }
  void router.push(`/sessions/${sessionId}`);
}

function handleCardContextMenuBeforeShow(sessionId: string, event: Event) {
  if (event.type !== 'touchstart') return;
  clearCardClickSuppression();
  suppressedCardClickId = sessionId;
}

function releaseCardContextMenuTouch(sessionId: string) {
  if (suppressedCardClickId !== sessionId) return;
  cardClickSuppressionTimer = setTimeout(() => clearCardClickSuppression(sessionId), 500);
}

function clearCardClickSuppression(sessionId?: string) {
  if (sessionId && suppressedCardClickId !== sessionId) return;
  suppressedCardClickId = '';
  if (cardClickSuppressionTimer) {
    clearTimeout(cardClickSuppressionTimer);
    cardClickSuppressionTimer = null;
  }
}

function cardAction(card: SessionCard) {
  if (
    card.pendingQuestion ||
    card.status === 'waiting_user' ||
    card.status === 'waiting_approval' ||
    card.status === 'closed'
  ) {
    return null;
  }
  if (card.status === 'starting' || card.status === 'running') {
    return { icon: 'stop', color: 'negative', tooltip: '运行中，点击停止', disabled: false };
  }
  if (card.status === 'stopping') {
    return { icon: 'hourglass_top', color: 'warning', tooltip: '停止中', disabled: true };
  }
  if (card.status === 'queued' && card.availableActions.includes('execute')) {
    return { icon: 'play_arrow', color: 'positive', tooltip: '强制启动排队卡片', disabled: false };
  }
  if (card.availableActions.includes('execute')) {
    return { icon: 'play_arrow', color: 'positive', tooltip: '运行会话', disabled: false };
  }
  return null;
}

async function runCardAction(card: SessionCard) {
  const action = cardAction(card);
  if (!action || action.disabled) return;
  activeActionSessionId.value = card.id;
  cardActionLoading.value = true;
  try {
    if (card.status === 'starting' || card.status === 'running') {
      await stopSession(card.id);
    } else if (card.availableActions.includes('execute')) {
      await executeSession(card.id, card.status === 'queued');
    }
    await loadOverviewSessions();
  } finally {
    cardActionLoading.value = false;
    activeActionSessionId.value = '';
  }
}

async function cancelQueuedCard(card: SessionCard) {
  if (card.status !== 'queued' || !card.availableActions.includes('stop')) return;
  activeActionSessionId.value = card.id;
  cardActionLoading.value = true;
  try {
    await stopSession(card.id);
    await loadOverviewSessions();
  } catch {
    await loadOverviewSessions().catch(() => undefined);
  } finally {
    cardActionLoading.value = false;
    activeActionSessionId.value = '';
  }
}

async function setCardPriority(card: SessionCard, priority: SessionPriority) {
  if (card.status === 'closed' || card.priority === priority || activePrioritySessionId.value)
    return;
  activePrioritySessionId.value = card.id;
  try {
    await updateSessionPriority(card.id, priority);
    await loadOverviewSessions();
  } finally {
    activePrioritySessionId.value = '';
  }
}

async function closeCard(card: SessionCard) {
  if (!card.availableActions.includes('close') || activeCloseSessionId.value) return;
  activeCloseSessionId.value = card.id;
  try {
    await closeSession(card.id);
    await loadOverviewSessions();
  } finally {
    activeCloseSessionId.value = '';
  }
}

async function openAnswerDialog(sessionId: string) {
  activeQuestionSessionId.value = sessionId;
  questionsLoading.value = true;
  answerDiffLoading.value = true;
  answerDiffs.value = [];
  answerDiffAvailable.value = false;
  answerDiffTotal.value = 0;
  answerDiffError.value = '';
  try {
    const [questionsResult, diffResult] = await Promise.allSettled([
      getPendingQuestionBatches(sessionId),
      getSessionAllDiff({ sessionId, mode: 'all', page: 1, pageSize: 20 }),
    ]);
    if (questionsResult.status === 'rejected') throw questionsResult.reason;
    pendingQuestionBatches.value = questionsResult.value;
    if (diffResult.status === 'fulfilled') {
      answerDiffAvailable.value = diffResult.value.available;
      answerDiffs.value = diffResult.value.allDiff;
      answerDiffTotal.value = diffResult.value.pageInfo.total;
    } else {
      answerDiffError.value = 'Diff 加载失败，请稍后刷新重试';
    }
    answerDialog.value = true;
  } finally {
    questionsLoading.value = false;
    answerDiffLoading.value = false;
  }
}

async function submitAnswers(batchId: string, answers: QuestionAnswerInput[]) {
  questionsSubmitting.value = true;
  try {
    await submitQuestionBatch(batchId, answers);
    if (activeQuestionSessionId.value) {
      pendingQuestionBatches.value = await getPendingQuestionBatches(activeQuestionSessionId.value);
    }
    answerDialog.value = pendingQuestionBatches.value.length > 0;
    await loadOverviewSessions();
  } finally {
    questionsSubmitting.value = false;
  }
}

async function openApprovalDialog(card: SessionCard) {
  const sessionId = card.id;
  const requestGeneration = ++approvalContextGeneration;
  approvalSessionId.value = sessionId;
  approvalLoading.value = true;
  approvalTab.value = 'output';
  approvalContext.value = card.pendingApproval
    ? { workflowRunId: card.pendingApproval.workflowRunId, nodeId: card.pendingApproval.nodeId }
    : null;
  approvalPending.value = card.pendingApproval ?? null;
  approvalDiffs.value = [];
  approvalDiffAvailable.value = false;
  approvalDiffTotal.value = 0;
  approvalDiffError.value = '';
  approvalDialog.value = true;
  try {
    try {
      const diff = await getSessionAllDiff({ sessionId, mode: 'all', page: 1, pageSize: 20 });
      if (!isCurrentApprovalContext(requestGeneration, sessionId)) return;
      approvalDiffAvailable.value = diff.available;
      approvalDiffs.value = diff.allDiff;
      approvalDiffTotal.value = diff.pageInfo.total;
    } catch {
      if (!isCurrentApprovalContext(requestGeneration, sessionId)) return;
      approvalDiffError.value = 'Diff 加载失败，请稍后刷新重试';
    }
  } finally {
    if (isCurrentApprovalContext(requestGeneration, sessionId)) {
      approvalLoading.value = false;
    }
  }
}

function isCurrentApprovalContext(requestGeneration: number, sessionId: string) {
  return approvalContextGeneration === requestGeneration && approvalSessionId.value === sessionId;
}

function openDiffDialog(card: SessionCard) {
  diffDialogSessionId.value = card.id;
  diffDialogWorkspaceState.value = { mode: 'all', filePath: '', page: 1, pageSize: 20 };
  diffDialog.value = true;
}

function handleDiffDialogClosed() {
  if (diffDialogSessionId.value) {
    void diffSummaryController.refresh([diffDialogSessionId.value]).catch(() => undefined);
  }
}

async function submitApproval(approved: boolean, comment: string) {
  if (approvalSubmitting.value) return;
  if (
    !approvalContext.value ||
    !approvalSessionId.value ||
    !isPendingApprovalReviewable(approvalPending.value)
  ) return;
  const requestGeneration = approvalContextGeneration;
  const sessionId = approvalSessionId.value;
  const workflowRunId = approvalContext.value.workflowRunId;
  const nodeId = approvalContext.value.nodeId;
  approvalSubmitting.value = true;
  try {
    await submitWorkflowApproval({
      workflowRunId,
      nodeId,
      approved,
      comment,
    });
    if (
      isCurrentApprovalContext(requestGeneration, sessionId) &&
      approvalContext.value?.workflowRunId === workflowRunId &&
      approvalContext.value?.nodeId === nodeId
    ) {
      approvalDialog.value = false;
    }
    await loadOverviewSessions();
  } finally {
    approvalSubmitting.value = false;
  }
}
</script>
