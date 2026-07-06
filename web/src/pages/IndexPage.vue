<template>
  <q-page class="workbench-page page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">{{ pageTitle }}</div>
        <div class="text-body2 text-muted">最近 3 天运行卡片与近 7 天历史记录</div>
      </div>
    </div>

    <div v-if="hasPlainCards" class="plain-card-groups q-mt-sm">
      <div v-if="plainRecentCards.length > 0" class="lane-section">
        <div class="lane-section__heading">
          <div class="text-subtitle2 text-weight-bold">最新</div>
          <div class="text-caption text-muted">最近 3 天或未结束</div>
        </div>
        <div class="lane-card-flow">
          <q-card
            v-for="card in plainRecentCards"
            :key="card.id"
            flat
            bordered
            clickable
            class="session-card lane-session-card"
            @click="$router.push(`/sessions/${card.id}`)"
          >
            <q-card-section class="lane-session-card__body">
              <div class="lane-card-content">
                <div class="lane-card-chips">
                  <q-badge
                    rounded
                    class="lane-status-chip"
                    :class="statusChipClass(card.status)"
                    :label="statusLabel(card.status)"
                  />
                  <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
                  <q-badge rounded class="lane-mode-chip" :label="priorityLabel(card.priority)" />
                </div>
                <div class="lane-card-title">{{ card.title }}</div>
                <div class="lane-card-meta">当前节点：{{ card.node }}</div>
                <div class="lane-card-meta">
                  最近操作 {{ card.updatedAt }} · 待回答 {{ card.pendingQuestion ? 1 : 0 }}
                </div>
                <div class="lane-card-actions">
                  <q-btn
                    v-if="card.pendingQuestion"
                    flat
                    dense
                    class="lane-icon-btn lane-icon-btn--warning"
                    icon="help"
                    aria-label="回答问题"
                    :loading="questionsLoading && activeQuestionSessionId === card.id"
                    @click.stop="openAnswerDialog(card.id)"
                  >
                    <q-tooltip>回答待处理问题</q-tooltip>
                  </q-btn>
                  <q-btn
                    v-if="cardAction(card)"
                    flat
                    dense
                    class="lane-icon-btn"
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
                    class="lane-icon-btn"
                    color="primary"
                    icon="chevron_right"
                    aria-label="打开卡片"
                    @click.stop="$router.push(`/sessions/${card.id}`)"
                  >
                    <q-tooltip>打开卡片详情</q-tooltip>
                  </q-btn>
                </div>
              </div>
            </q-card-section>
          </q-card>
        </div>
      </div>

      <div v-if="plainHistoryCards.length > 0" class="lane-section q-mt-md">
        <div class="lane-section__heading">
          <div class="text-subtitle2 text-weight-bold">历史</div>
          <div class="text-caption text-muted">结束时间倒序</div>
        </div>
        <div class="lane-card-flow">
          <q-card
            v-for="card in plainHistoryCards"
            :key="card.id"
            flat
            bordered
            clickable
            class="session-card lane-session-card"
            @click="$router.push(`/sessions/${card.id}`)"
          >
            <q-card-section class="lane-session-card__body">
              <div class="lane-card-content">
                <div class="lane-card-chips">
                  <q-badge
                    rounded
                    class="lane-status-chip"
                    :class="statusChipClass(card.status)"
                    :label="statusLabel(card.status)"
                  />
                  <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
                  <q-badge rounded class="lane-mode-chip" :label="priorityLabel(card.priority)" />
                </div>
                <div class="lane-card-title">{{ card.title }}</div>
                <div class="lane-card-meta">当前节点：{{ card.node }}</div>
                <div class="lane-card-meta">结束 {{ card.updatedAt }}</div>
                <div class="lane-card-actions">
                  <q-btn
                    v-if="card.pendingQuestion"
                    flat
                    dense
                    class="lane-icon-btn lane-icon-btn--warning"
                    icon="help"
                    aria-label="回答问题"
                    :loading="questionsLoading && activeQuestionSessionId === card.id"
                    @click.stop="openAnswerDialog(card.id)"
                  >
                    <q-tooltip>回答待处理问题</q-tooltip>
                  </q-btn>
                  <q-btn
                    v-if="cardAction(card)"
                    flat
                    dense
                    class="lane-icon-btn"
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
                    class="lane-icon-btn"
                    color="primary"
                    icon="chevron_right"
                    aria-label="打开卡片"
                    @click.stop="$router.push(`/sessions/${card.id}`)"
                  >
                    <q-tooltip>打开卡片详情</q-tooltip>
                  </q-btn>
                </div>
              </div>
            </q-card-section>
          </q-card>
          <router-link v-if="hasMorePlainHistory" class="lane-more-card" :to="{ name: 'sessions' }">
            <q-icon name="history" />
            <span>更多历史进入表格</span>
          </router-link>
        </div>
      </div>
    </div>

    <div class="branch-lanes q-mt-sm">
      <q-expansion-item
        v-for="lane in branchLanes"
        :key="lane.id"
        default-opened
        class="branch-lane"
        header-class="branch-lane__header"
      >
        <template #header>
          <q-item-section>
            <div class="row items-center q-gutter-sm">
              <span class="text-subtitle1 text-weight-bold">{{ lane.branch }}</span>
              <q-chip v-if="!projectScopeId" dense square outline color="blue-grey">{{
                lane.projectName
              }}</q-chip>
              <span class="text-caption text-muted">{{ lane.statusText }}</span>
            </div>
          </q-item-section>
          <q-item-section side>
            <div class="row items-center q-gutter-md no-wrap">
              <router-link class="stat-link" :to="lane.commitRoute"
                >提交 {{ lane.commitCount }}</router-link
              >
              <router-link class="stat-link" :to="lane.diffRoute"
                >未提交 {{ lane.uncommittedCount }}</router-link
              >
              <span class="text-caption text-muted"
                >最新 {{ lane.recent.length }} · 历史 {{ lane.history.length }}</span
              >
            </div>
          </q-item-section>
        </template>

        <div class="branch-lane__body">
          <div class="lane-section">
            <div class="lane-section__heading">
              <div class="text-subtitle2 text-weight-bold">最新</div>
              <div class="text-caption text-muted">最近 3 天或未结束</div>
            </div>
            <div v-if="lane.recent.length > 0" class="lane-card-flow">
              <q-card
                v-for="card in lane.recent"
                :key="card.id"
                flat
                bordered
                clickable
                class="session-card lane-session-card"
                @click="$router.push(`/sessions/${card.id}`)"
              >
                <q-card-section class="lane-session-card__body">
                  <div class="lane-card-content">
                    <div class="lane-card-chips">
                      <q-badge
                        rounded
                        class="lane-status-chip"
                        :class="statusChipClass(card.status)"
                        :label="statusLabel(card.status)"
                      />
                      <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
                      <q-badge
                        rounded
                        class="lane-mode-chip"
                        :label="priorityLabel(card.priority)"
                      />
                    </div>
                    <div class="lane-card-title">{{ card.title }}</div>
                    <div class="lane-card-meta">当前节点：{{ card.node }}</div>
                    <div class="lane-card-meta">
                      最近操作 {{ card.updatedAt }} · 待回答 {{ card.pendingQuestion ? 1 : 0 }}
                    </div>
                    <div class="lane-card-actions">
                      <q-btn
                        v-if="card.pendingQuestion"
                        flat
                        dense
                        class="lane-icon-btn lane-icon-btn--warning"
                        icon="help"
                        aria-label="回答问题"
                        :loading="questionsLoading && activeQuestionSessionId === card.id"
                        @click.stop="openAnswerDialog(card.id)"
                      >
                        <q-tooltip>回答待处理问题</q-tooltip>
                      </q-btn>
                      <q-btn
                        v-if="cardAction(card)"
                        flat
                        dense
                        class="lane-icon-btn"
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
                        class="lane-icon-btn"
                        color="primary"
                        icon="chevron_right"
                        aria-label="打开卡片"
                        @click.stop="$router.push(`/sessions/${card.id}`)"
                      >
                        <q-tooltip>打开卡片详情</q-tooltip>
                      </q-btn>
                    </div>
                  </div>
                </q-card-section>
              </q-card>
            </div>
            <q-banner v-else dense rounded class="empty-lane-banner">暂无最新卡片</q-banner>
          </div>

          <div class="lane-section q-mt-md">
            <div class="lane-section__heading">
              <div class="text-subtitle2 text-weight-bold">历史</div>
              <div class="text-caption text-muted">结束时间倒序，每泳道最多 10 张</div>
            </div>
            <div v-if="lane.history.length > 0" class="lane-card-flow">
              <q-card
                v-for="card in lane.history"
                :key="card.id"
                flat
                bordered
                clickable
                class="session-card lane-session-card"
                @click="$router.push(`/sessions/${card.id}`)"
              >
                <q-card-section class="lane-session-card__body">
                  <div class="lane-card-content">
                    <div class="lane-card-chips">
                      <q-badge
                        rounded
                        class="lane-status-chip"
                        :class="statusChipClass(card.status)"
                        :label="statusLabel(card.status)"
                      />
                      <q-badge rounded class="lane-mode-chip" :label="modeLabel(card.mode)" />
                      <q-badge
                        rounded
                        class="lane-mode-chip"
                        :label="priorityLabel(card.priority)"
                      />
                    </div>
                    <div class="lane-card-title">{{ card.title }}</div>
                    <div class="lane-card-meta">当前节点：{{ card.node }}</div>
                    <div class="lane-card-meta">结束 {{ card.updatedAt }}</div>
                    <div class="lane-card-actions">
                      <q-btn
                        v-if="card.pendingQuestion"
                        flat
                        dense
                        class="lane-icon-btn lane-icon-btn--warning"
                        icon="help"
                        aria-label="回答问题"
                        :loading="questionsLoading && activeQuestionSessionId === card.id"
                        @click.stop="openAnswerDialog(card.id)"
                      >
                        <q-tooltip>回答待处理问题</q-tooltip>
                      </q-btn>
                      <q-btn
                        v-if="cardAction(card)"
                        flat
                        dense
                        class="lane-icon-btn"
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
                        class="lane-icon-btn"
                        color="primary"
                        icon="chevron_right"
                        aria-label="打开卡片"
                        @click.stop="$router.push(`/sessions/${card.id}`)"
                      >
                        <q-tooltip>打开卡片详情</q-tooltip>
                      </q-btn>
                    </div>
                  </div>
                </q-card-section>
              </q-card>
              <router-link
                v-if="lane.hasMoreHistory"
                class="lane-more-card"
                :to="{ name: 'sessions' }"
              >
                <q-icon name="history" />
                <span>更多历史进入表格</span>
              </router-link>
            </div>
            <q-banner v-else dense rounded class="empty-lane-banner">暂无历史卡片</q-banner>
          </div>
        </div>
      </q-expansion-item>
    </div>

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
import { getBranchDiff, getSessionCommitHistory } from '@/services/diff';
import {
  getPendingQuestionBatches,
  resumeSession,
  startSession,
  stopSession,
  submitQuestionBatch,
  type QuestionAnswerInput,
  type QuestionBatch,
  type SessionCard,
  type SessionMode,
  type SessionStatus,
} from '@/services/sessions';

const route = useRoute();
const projectScopeId = computed(() => {
  const value = route.query.projectId;
  return typeof value === 'string' ? value : '';
});

const {
  rows: recentRows,
  loadSessions: loadRecentSessions,
  startLiveUpdates: startRecentLiveUpdates,
  stopLiveUpdates: stopRecentLiveUpdates,
} = useSessionsPage({
  projectId: projectScopeId.value,
  range: 'recent3d',
  page: 1,
  pageSize: 100,
  sort: 'updated_at desc',
  loadAll: true,
});
const {
  rows: historyRows,
  loadSessions: loadHistorySessions,
  startLiveUpdates: startHistoryLiveUpdates,
  stopLiveUpdates: stopHistoryLiveUpdates,
} = useSessionsPage({
  projectId: projectScopeId.value,
  range: 'history7d',
  page: 1,
  pageSize: 100,
  sort: 'updated_at desc',
  loadAll: true,
});
const { projects, loadProjects } = useProjects();

const recentCards = computed(() => recentRows.value);
const historyCards = computed(() => historyRows.value);
const scopedProject = computed(() =>
  projects.value.find((project) => project.id === projectScopeId.value),
);
const pageTitle = computed(() => scopedProject.value?.name ?? '总揽');
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);
const cardActionLoading = ref(false);
const activeActionSessionId = ref('');
const branchDiffCounts = ref<Record<string, number>>({});
const commitCounts = ref<Record<string, number>>({});
const OTHER_BRANCH_NAME = '其他';

interface BranchLane {
  id: string;
  projectId: string;
  projectName: string;
  branch: string;
  statusText: string;
  commitCount: number;
  uncommittedCount: number;
  recent: SessionCard[];
  history: SessionCard[];
  hasMoreHistory: boolean;
  commitRoute: { name: string; params: Record<string, string> };
  diffRoute: { name: string; query: Record<string, string> };
  firstIndex: number;
}

const branchLanes = computed(() => {
  const recentIds = new Set(recentCards.value.map((card) => card.id));
  const orderedCards = [
    ...recentCards.value
      .filter((card) => isGitProject(card.projectId))
      .map((card) => ({ card, section: 'recent' as const })),
    ...historyCards.value
      .filter((card) => !recentIds.has(card.id))
      .filter((card) => isGitProject(card.projectId))
      .map((card) => ({ card, section: 'history' as const })),
  ];
  const lanes = new Map<string, BranchLane>();

  orderedCards.forEach(({ card, section }, index) => {
    const branch = card.branch || OTHER_BRANCH_NAME;
    const laneKey = `${card.projectId}:${branch}`;
    let lane = lanes.get(laneKey);
    if (!lane) {
      lane = {
        id: laneKey,
        projectId: card.projectId,
        projectName: card.projectName,
        branch,
        statusText: `最近操作 ${card.updatedAt}`,
        commitCount: 0,
        uncommittedCount: 0,
        recent: [],
        history: [],
        hasMoreHistory: false,
        commitRoute: { name: 'session-commits', params: { id: card.id } },
        diffRoute: { name: 'diff', query: { projectId: card.projectId, branch, mode: 'all' } },
        firstIndex: index,
      };
      lanes.set(laneKey, lane);
    }

    const cardCommitCount = commitCounts.value[card.id] ?? 0;
    if (cardCommitCount > 0 && lane.commitCount === 0) {
      lane.commitRoute = { name: 'session-commits', params: { id: card.id } };
    }
    lane.commitCount += cardCommitCount;
    lane.uncommittedCount = branchDiffCounts.value[laneKey] ?? 0;
    if (section === 'recent') {
      lane.recent.push(card);
      return;
    }
    if (lane.history.length < 10) {
      lane.history.push(card);
    } else {
      lane.hasMoreHistory = true;
    }
  });

  return Array.from(lanes.values()).sort((left, right) => left.firstIndex - right.firstIndex);
});

const projectsById = computed(
  () => new Map(projects.value.map((project) => [project.id, project])),
);
const uniqueHistoryCards = computed(() => {
  const recentIds = new Set(recentCards.value.map((card) => card.id));
  return historyCards.value.filter((card) => !recentIds.has(card.id));
});
const plainRecentCards = computed(() =>
  recentCards.value.filter((card) => !isGitProject(card.projectId)),
);
const allPlainHistoryCards = computed(() =>
  uniqueHistoryCards.value.filter((card) => !isGitProject(card.projectId)),
);
const plainHistoryCards = computed(() => allPlainHistoryCards.value.slice(0, 10));
const hasMorePlainHistory = computed(
  () => allPlainHistoryCards.value.length > plainHistoryCards.value.length,
);
const hasPlainCards = computed(
  () => plainRecentCards.value.length > 0 || plainHistoryCards.value.length > 0,
);
const hasVisibleCards = computed(() => branchLanes.value.length > 0 || hasPlainCards.value);

onMounted(() => {
  void startOverview();
});

onUnmounted(() => {
  stopRecentLiveUpdates();
  stopHistoryLiveUpdates();
});

watch(
  () => [...recentRows.value, ...historyRows.value].map((card) => card.id).join(','),
  () => {
    void loadLaneStats();
  },
);

async function startOverview() {
  await loadProjects();
  await loadOverviewSessions();
  startRecentLiveUpdates();
  startHistoryLiveUpdates();
}

async function loadOverviewSessions() {
  await Promise.all([loadRecentSessions(), loadHistorySessions()]);
  await loadLaneStats();
}

async function loadLaneStats() {
  await Promise.all([loadBranchDiffCounts(), loadCommitCounts()]);
}

async function loadBranchDiffCounts() {
  const lanes = Array.from(
    new Map(
      [...recentRows.value, ...historyRows.value]
        .filter((card) => isGitProject(card.projectId))
        .map((card) => {
          const branch = card.branch || OTHER_BRANCH_NAME;
          return [`${card.projectId}:${branch}`, { projectId: card.projectId, branch }] as const;
        }),
    ).entries(),
  );
  if (lanes.length === 0) {
    branchDiffCounts.value = {};
    return;
  }
  const next = { ...branchDiffCounts.value };
  await Promise.all(
    lanes.map(async ([key, lane]) => {
      try {
        const diff = await getBranchDiff({
          projectId: lane.projectId,
          branch: lane.branch,
          mode: 'single',
          page: 1,
          pageSize: 1,
        });
        next[key] = diff.available ? diff.pageInfo.total : 0;
      } catch {
        next[key] = 0;
      }
    }),
  );
  branchDiffCounts.value = next;
}

function isGitProject(projectId: string) {
  return projectsById.value.get(projectId)?.isGit ?? true;
}

async function loadCommitCounts() {
  const ids = Array.from(
    new Set([...recentRows.value, ...historyRows.value].map((card) => card.id)),
  );
  if (ids.length === 0) {
    commitCounts.value = {};
    return;
  }
  const next = { ...commitCounts.value };
  await Promise.all(
    ids.map(async (id) => {
      try {
        const history = await getSessionCommitHistory({ sessionId: id, page: 1, pageSize: 1 });
        next[id] = history.available ? history.pageInfo.total : 0;
      } catch {
        next[id] = 0;
      }
    }),
  );
  commitCounts.value = next;
}

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function priorityLabel(priority: SessionCard['priority']) {
  const labels: Record<SessionCard['priority'], string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
  };
  return labels[priority];
}

function statusChipClass(status: SessionStatus) {
  return `lane-status-chip--${status}`;
}

function statusLabel(status: SessionStatus) {
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
  return labels[status];
}

function cardAction(card: SessionCard) {
  if (card.pendingQuestion || card.status === 'waiting_user') return null;
  if (card.status === 'starting' || card.status === 'running') {
    return { icon: 'stop', color: 'negative', tooltip: '运行中，点击停止', disabled: false };
  }
  if (card.status === 'stopping') {
    return { icon: 'hourglass_top', color: 'warning', tooltip: '停止中', disabled: true };
  }
  if (card.status === 'queued' && card.availableActions.includes('run')) {
    return { icon: 'play_arrow', color: 'positive', tooltip: '强制启动排队卡片', disabled: false };
  }
  if (card.availableActions.includes('run')) {
    return { icon: 'play_arrow', color: 'positive', tooltip: '强制运行', disabled: false };
  }
  if (card.availableActions.includes('resume')) {
    return { icon: 'restart_alt', color: 'primary', tooltip: '恢复会话', disabled: false };
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
    } else if (card.availableActions.includes('run')) {
      await startSession(card.id, card.status === 'queued');
    } else if (card.availableActions.includes('resume')) {
      await resumeSession(card.id, card.status === 'queued');
    }
    await loadOverviewSessions();
  } finally {
    cardActionLoading.value = false;
    activeActionSessionId.value = '';
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
    await Promise.all([loadRecentSessions(), loadHistorySessions()]);
  } finally {
    questionsSubmitting.value = false;
  }
}
</script>
