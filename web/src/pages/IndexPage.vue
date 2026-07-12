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
          @keyup.enter="$router.push(`/sessions/${card.id}`)"
          @keyup.space.prevent="$router.push(`/sessions/${card.id}`)"
        >
          <q-menu context-menu>
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
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useSessionsPage } from '@/composables/useSessionsPage';
import {
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { createOverviewCardGroups } from '@/services/overviewCardGroups';
import { shouldReconnectCardStream } from '@/services/sessionEventTimeline';
import { sessionStatusLabel as statusLabel } from '@/services/sessionStatusPresentation';
import {
  closeSession,
  executeSession,
  getPendingQuestionBatches,
  stopSession,
  subscribeSessionCardChanged,
  submitQuestionBatch,
  updateSessionPriority,
  type QuestionAnswerInput,
  type QuestionBatch,
  type SessionCard,
  type SessionMode,
  type SessionPriority,
  type SessionStatus,
} from '@/services/sessions';

const route = useRoute();
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

const overviewCardGroups = computed(() =>
  createOverviewCardGroups(latestRows.value, historyRows.value),
);
const latestCards = computed(() => overviewCardGroups.value.latestCards);
const uniqueHistoryCards = computed(() => overviewCardGroups.value.historyCards);
const historyCards = computed(() => uniqueHistoryCards.value.slice(0, 10));
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
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);
const cardActionLoading = ref(false);
const activeActionSessionId = ref('');
const activePrioritySessionId = ref('');
const activeCloseSessionId = ref('');
const priorities: SessionPriority[] = ['high', 'medium', 'low'];
let cardSubscription: ReturnType<typeof subscribeSessionCardChanged> | null = null;
let cardReconnectTimer: ReturnType<typeof setTimeout> | null = null;
let cardRefreshTimer: ReturnType<typeof setTimeout> | null = null;
let liveStopped = true;

onMounted(() => {
  void startOverview();
});

onUnmounted(() => {
  stopOverviewLiveUpdates();
});

watch(projectScopeId, (value) => {
  latestProjectId.value = value;
  historyProjectId.value = value;
  void loadOverviewSessions();
  if (!liveStopped) {
    startOverviewLiveUpdates();
  }
});

async function startOverview() {
  await loadProjects();
  await loadOverviewSessions();
  startOverviewLiveUpdates();
}

async function loadOverviewSessions() {
  await Promise.all([loadLatestSessions(), loadHistorySessions()]);
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
  if (card.pendingQuestion || card.status === 'waiting_user' || card.status === 'closed') {
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
</script>
