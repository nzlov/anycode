<template>
  <q-page class="workbench-page page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">{{ pageTitle }}</div>
        <div class="text-body2 text-muted">最新卡片与历史卡片</div>
      </div>
    </div>

    <section v-for="section in cardSections" :key="section.id" class="overview-card-section">
      <div class="overview-card-section__heading">
        <div>
          <div class="text-subtitle1 text-weight-bold">{{ section.title }}</div>
          <div class="text-caption text-muted">{{ section.caption }}</div>
        </div>
      </div>

      <div v-if="section.cards.length > 0" class="overview-card-grid">
        <q-card
          v-for="card in section.cards"
          :key="card.id"
          flat
          bordered
          clickable
          tabindex="0"
          class="overview-session-card cursor-pointer"
          :class="overviewCardClass(card)"
          @click="$router.push(`/sessions/${card.id}`)"
          @keyup.enter.self="$router.push(`/sessions/${card.id}`)"
          @keyup.space.self.prevent="$router.push(`/sessions/${card.id}`)"
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
                @keyup.enter.stop
                @keyup.space.stop
              >
                <q-tooltip>TODO List</q-tooltip>
                <q-menu anchor="top left" self="bottom left" class="overview-todo-menu" @click.stop>
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

              <div class="overview-card-actions">
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
                <q-btn
                  flat
                  dense
                  class="lane-icon-btn app-icon-btn"
                  icon="more_vert"
                  aria-label="卡片操作"
                  @click.stop
                >
                  <q-menu anchor="top right" self="bottom right" @click.stop>
                    <q-list dense class="overview-card-menu app-touch-list">
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
                </q-btn>
              </div>
            </div>
          </q-card-section>
        </q-card>

        <router-link v-if="section.showMore" class="overview-more-card" :to="sessionsRoute">
          <q-icon name="history" />
          <span>更多历史进入表格</span>
        </router-link>
      </div>
      <q-banner v-else dense rounded class="empty-lane-banner">暂无{{ section.title }}</q-banner>
    </section>

    <q-banner v-if="!hasVisibleCards" rounded class="empty-lane-banner q-mt-md">
      暂无卡片
    </q-banner>

    <AnswerUserDialog
      v-model="answerDialog"
      :batches="pendingQuestionBatches"
      :loading="questionsLoading"
      :submitting="questionsSubmitting"
      @submit="submitAnswers"
    />

    <q-dialog v-model="approvalDialog" :maximized="$q.screen.lt.sm">
      <q-card class="forward-approval-dialog app-content-dialog">
        <div class="forward-approval-dialog__tabs">
          <q-tabs v-model="approvalTab" dense align="left" class="text-primary">
            <q-tab name="output" icon="smart_toy" label="模型输出" />
            <q-tab name="diff" icon="difference" label="Diff" />
          </q-tabs>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭人工审核" />
        </div>
        <q-separator />
        <q-tab-panels v-model="approvalTab" animated class="forward-approval-dialog__panels">
          <q-tab-panel name="output" class="forward-approval-dialog__panel">
            <q-banner
              v-if="approvalOutputError"
              dense
              rounded
              class="state-banner bg-negative text-white"
            >
              {{ approvalOutputError }}
            </q-banner>
            <div v-else-if="approvalMessages.length === 0" class="text-muted">暂无模型输出</div>
            <div v-else class="forward-approval-output">
              <SessionEventMessage
                v-for="message in approvalMessages"
                :key="message.id"
                :event="message"
              />
            </div>
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
          :context-available="Boolean(approvalContext)"
          :submitting="approvalSubmitting"
          @submit="submitApproval"
        />
      </q-card>
    </q-dialog>

    <q-dialog v-model="diffDialog" :maximized="$q.screen.lt.sm" @hide="handleDiffDialogClosed">
      <q-card class="overview-diff-dialog app-content-dialog">
        <div class="overview-diff-dialog__header">
          <div class="text-subtitle1 text-weight-bold">Diff</div>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭 Diff" />
        </div>
        <q-separator />
        <q-card-section class="overview-diff-dialog__body">
          <SessionDiffPreview
            :loading="diffDialogLoading"
            :error="diffDialogError"
            :available="diffDialogAvailable"
            :file-diffs="diffDialogDiffs"
            :total="diffDialogTotal"
            :full-diff-route="diffDialogAllDiffRoute"
          />
        </q-card-section>
      </q-card>
    </q-dialog>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute } from 'vue-router';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import SessionDiffPreview from '@/components/SessionDiffPreview.vue';
import SessionEventMessage from '@/components/SessionEventMessage.vue';
import WorkflowApprovalPanel from '@/components/WorkflowApprovalPanel.vue';
import { useProjects } from '@/composables/useProjects';
import { useSessionsPage } from '@/composables/useSessionsPage';
import {
  getSessionAllDiff,
  getSessionDiffSummaries,
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
import { getSessionTranscriptPage, type TranscriptItem } from '@/services/sessionTimeline';
import { reduceTranscriptEvents } from '@/services/sessionTimelineReducer';
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
  type SessionCard,
  type SessionMode,
  type SessionPriority,
  type SessionStatus,
} from '@/services/sessions';

const route = useRoute();
const $q = useQuasar();
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
const {
  rows: historyRows,
  projectId: historyProjectId,
  loadSessions: loadHistorySessions,
} = useSessionsPage({
  projectId: projectScopeId.value,
  range: 'history',
  page: 1,
  pageSize: 100,
  sort: 'updated_at desc',
  loadAll: true,
});
const { projects, loadProjects } = useProjects();

const diffSummariesBySessionId = ref<Record<string, SessionDiffSummary>>({});
const overviewCardGroups = computed(() =>
  createOverviewCardGroups(latestRows.value, historyRows.value),
);
const latestCards = computed(() => overviewCardGroups.value.latestCards.map(withDiffSummary));
const uniqueHistoryCards = computed(() => overviewCardGroups.value.historyCards);
const historyCards = computed(() => uniqueHistoryCards.value.slice(0, 10).map(withDiffSummary));
const hasMoreHistory = computed(() => uniqueHistoryCards.value.length > historyCards.value.length);
const scopedProject = computed(() =>
  projects.value.find((project) => project.id === projectScopeId.value),
);
const pageTitle = computed(() => scopedProject.value?.name ?? '总揽');
const sessionsRoute = computed(() =>
  projectScopeId.value
    ? { name: 'sessions', query: { projectId: projectScopeId.value, scope: 'closed' } }
    : { name: 'sessions', query: { scope: 'closed' } },
);
const cardSections = computed(() => [
  {
    id: 'latest',
    title: '最新',
    caption: '未关闭的卡片，按最近操作倒序',
    cards: latestCards.value,
    showMore: false,
  },
  {
    id: 'history',
    title: '历史',
    caption: '已关闭的卡片，按最近操作倒序',
    cards: historyCards.value,
    showMore: hasMoreHistory.value,
  },
]);
const hasVisibleCards = computed(
  () => latestCards.value.length > 0 || historyCards.value.length > 0,
);
const visibleCards = computed(() => [...latestCards.value, ...historyCards.value]);
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);
const approvalDialog = ref(false);
const approvalTab = ref<'output' | 'diff'>('output');
const approvalLoading = ref(false);
const approvalSubmitting = ref(false);
const approvalSessionId = ref('');
const approvalContext = ref<ApprovalContext | null>(null);
const approvalMessages = ref<TranscriptItem[]>([]);
const approvalOutputError = ref('');
const approvalDiffs = ref<FileDiff[]>([]);
const approvalDiffAvailable = ref(false);
const approvalDiffTotal = ref(0);
const approvalDiffError = ref('');
const diffDialog = ref(false);
const diffDialogLoading = ref(false);
const diffDialogSessionId = ref('');
const diffDialogDiffs = ref<FileDiff[]>([]);
const diffDialogAvailable = ref(false);
const diffDialogTotal = ref(0);
const diffDialogError = ref('');
const cardActionLoading = ref(false);
const activeActionSessionId = ref('');
const activePrioritySessionId = ref('');
const activeCloseSessionId = ref('');
const priorities: SessionPriority[] = ['high', 'medium', 'low'];
const approvalAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: approvalSessionId.value, mode: 'all' },
}));
const diffDialogAllDiffRoute = computed(() => ({
  path: '/diff',
  query: { sessionId: diffDialogSessionId.value, mode: 'all' },
}));
let cardSubscription: ReturnType<typeof subscribeSessionCardChanged> | null = null;
let cardReconnectTimer: ReturnType<typeof setTimeout> | null = null;
let cardRefreshTimer: ReturnType<typeof setTimeout> | null = null;
let liveStopped = true;
let diffDialogRequestGeneration = 0;

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
  getVisibleCards: () => visibleCards.value,
  isPageVisible: () => typeof document === 'undefined' || document.visibilityState === 'visible',
});

onMounted(() => {
  document.addEventListener('visibilitychange', handleVisibilityChange);
  void startOverview();
});

onUnmounted(() => {
  document.removeEventListener('visibilitychange', handleVisibilityChange);
  diffSummaryController.stop();
  stopOverviewLiveUpdates();
});

watch(projectScopeId, (value) => {
  latestProjectId.value = value;
  historyProjectId.value = value;
  diffSummariesBySessionId.value = {};
  void loadOverviewSessions();
  if (!liveStopped) {
    startOverviewLiveUpdates();
  }
});

async function startOverview() {
  await loadProjects();
  await loadOverviewSessions();
  diffSummaryController.start();
  startOverviewLiveUpdates();
}

async function loadOverviewSessions() {
  await Promise.all([loadLatestSessions(), loadHistorySessions()]);
  await diffSummaryController
    .refresh(visibleCards.value.map((card) => card.id))
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

function handleVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void diffSummaryController
      .refresh(activeOverviewDiffSessionIds(visibleCards.value))
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
  try {
    pendingQuestionBatches.value = await getPendingQuestionBatches(sessionId);
    answerDialog.value = true;
  } finally {
    questionsLoading.value = false;
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
  approvalSessionId.value = card.id;
  approvalLoading.value = true;
  approvalTab.value = 'output';
  approvalContext.value = card.pendingApproval
    ? { workflowRunId: card.pendingApproval.workflowRunId, nodeId: card.pendingApproval.nodeId }
    : null;
  approvalMessages.value = [];
  approvalOutputError.value = '';
  approvalDiffs.value = [];
  approvalDiffAvailable.value = false;
  approvalDiffTotal.value = 0;
  approvalDiffError.value = '';
  try {
    const [timelineResult, diffResult] = await Promise.allSettled([
      getSessionTranscriptPage(card.id, '', 10, 'assistant'),
      getSessionAllDiff({ sessionId: card.id, mode: 'all', page: 1, pageSize: 20 }),
    ]);
    if (timelineResult.status === 'fulfilled') {
      approvalMessages.value = reduceTranscriptEvents(timelineResult.value.items);
    } else {
      approvalOutputError.value = '模型输出加载失败，请稍后刷新重试';
    }
    if (diffResult.status === 'fulfilled') {
      approvalDiffAvailable.value = diffResult.value.available;
      approvalDiffs.value = diffResult.value.allDiff;
      approvalDiffTotal.value = diffResult.value.pageInfo.total;
    } else {
      approvalDiffError.value = 'Diff 加载失败，请稍后刷新重试';
    }
    approvalDialog.value = true;
  } finally {
    approvalLoading.value = false;
  }
}

async function openDiffDialog(card: SessionCard) {
  const requestGeneration = ++diffDialogRequestGeneration;
  diffDialogSessionId.value = card.id;
  diffDialogLoading.value = true;
  diffDialogDiffs.value = [];
  diffDialogAvailable.value = false;
  diffDialogTotal.value = 0;
  diffDialogError.value = '';
  diffDialog.value = true;
  try {
    const result = await getSessionAllDiff({
      sessionId: card.id,
      mode: 'all',
      page: 1,
      pageSize: 20,
    });
    if (requestGeneration !== diffDialogRequestGeneration) return;
    diffDialogAvailable.value = result.available;
    diffDialogDiffs.value = result.allDiff;
    diffDialogTotal.value = result.pageInfo.total;
  } catch {
    if (requestGeneration !== diffDialogRequestGeneration) return;
    diffDialogError.value = 'Diff 加载失败，请稍后重试';
  } finally {
    if (requestGeneration === diffDialogRequestGeneration) {
      diffDialogLoading.value = false;
    }
  }
}

function handleDiffDialogClosed() {
  diffDialogRequestGeneration += 1;
  if (diffDialogSessionId.value) {
    void diffSummaryController.refresh([diffDialogSessionId.value]).catch(() => undefined);
  }
}

async function submitApproval(approved: boolean, comment: string) {
  if (!approvalContext.value || !approvalSessionId.value) return;
  approvalSubmitting.value = true;
  try {
    await submitWorkflowApproval({
      workflowRunId: approvalContext.value.workflowRunId,
      nodeId: approvalContext.value.nodeId,
      approved,
      comment,
    });
    approvalDialog.value = false;
    await loadOverviewSessions();
  } finally {
    approvalSubmitting.value = false;
  }
}
</script>
