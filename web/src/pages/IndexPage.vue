<template>
  <q-page class="workbench-page page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">总揽</div>
        <div class="text-body2 text-muted">最近 3 天运行卡片与近 7 天历史记录</div>
      </div>
    </div>

    <div class="stats-grid">
      <q-card v-for="stat in stats" :key="stat.label" flat bordered class="stat-card">
        <q-card-section>
          <div class="row items-center no-wrap">
            <q-icon :name="stat.icon" :color="stat.color" size="28px" />
            <q-space />
            <div class="text-h6 text-weight-bold">{{ stat.value }}</div>
          </div>
          <div class="text-caption text-muted q-mt-sm">{{ stat.label }}</div>
        </q-card-section>
      </q-card>
    </div>

    <div class="row q-col-gutter-lg q-mt-sm">
      <section class="col-12 col-lg-7">
        <div class="section-heading">
          <div class="text-subtitle1 text-weight-bold">最近</div>
          <q-chip dense outline color="primary" icon="schedule">3 天内</q-chip>
        </div>
        <div class="column q-gutter-md">
          <q-card
            v-for="card in recentCards"
            :key="card.id"
            flat
            bordered
            clickable
            class="session-card"
            @click="$router.push(`/sessions/${card.id}`)"
          >
            <q-card-section>
              <div class="row items-start q-col-gutter-md">
                <div class="col">
                  <div class="row items-center q-gutter-sm q-mb-xs">
                    <q-badge :color="statusColor(card.status)" rounded />
                    <span class="text-weight-bold">{{ card.title }}</span>
                    <q-badge outline color="primary" :label="modeLabel(card.mode)" />
                  </div>
                  <div class="text-body2 text-muted">{{ card.summary }}</div>
                  <q-separator class="q-my-sm" />
                  <div class="metadata-row">
                    <q-icon name="folder" />
                    <span>{{ card.projectName }}</span>
                    <q-icon name="account_tree" />
                    <span>{{ card.node }}</span>
                    <q-icon :name="statusIcon(card.status)" />
                    <span>{{ statusLabel(card.status) }}</span>
                  </div>
                </div>
                <div class="column items-end q-gutter-sm">
                  <q-btn
                    v-if="card.pendingQuestion"
                    round
                    unelevated
                    color="warning"
                    text-color="dark"
                    icon="help"
                    aria-label="回答问题"
                    :loading="questionsLoading && activeQuestionSessionId === card.id"
                    @click.stop="openAnswerDialog(card.id)"
                  >
                    <q-tooltip>回答问题</q-tooltip>
                  </q-btn>
                  <q-btn flat round color="primary" icon="chevron_right" aria-label="打开卡片" />
                </div>
              </div>
            </q-card-section>
          </q-card>
        </div>
      </section>

      <section class="col-12 col-lg-5">
        <div class="section-heading">
          <div class="text-subtitle1 text-weight-bold">历史</div>
          <div class="row items-center q-gutter-sm">
            <q-chip dense outline color="secondary" icon="history">近 7 天</q-chip>
            <q-btn flat dense color="primary" icon-right="chevron_right" label="更多" no-caps to="/sessions" />
          </div>
        </div>
        <q-list bordered separator class="surface-list">
          <q-item
            v-for="item in historyCards"
            :key="item.id"
            clickable
            :to="`/sessions/${item.id}`"
          >
            <q-item-section avatar>
              <q-icon :name="statusIcon(item.status)" :color="statusColor(item.status)" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ item.title }}</q-item-label>
              <q-item-label caption
                >{{ item.projectName }} · {{ item.updatedAt }}</q-item-label
              >
            </q-item-section>
            <q-item-section side>
              <q-btn
                v-if="item.pendingQuestion"
                dense
                flat
                color="warning"
                icon="help"
                label="待回答"
                no-caps
                :loading="questionsLoading && activeQuestionSessionId === item.id"
                @click.stop.prevent="openAnswerDialog(item.id)"
              />
              <q-badge v-else outline color="blue-grey" label="已同步" />
            </q-item-section>
          </q-item>
        </q-list>
      </section>
    </div>

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
import { computed, onMounted, onUnmounted, ref } from 'vue';

import AnswerUserDialog from '@/components/AnswerUserDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useSessionsPage } from '@/composables/useSessionsPage';
import {
  getPendingQuestionBatches,
  submitQuestionBatch,
  type QuestionAnswerInput,
  type QuestionBatch,
  type SessionMode,
  type SessionStatus,
} from '@/services/sessions';

const {
  rows: recentRows,
  loadSessions: loadRecentSessions,
  startLiveUpdates: startRecentLiveUpdates,
  stopLiveUpdates: stopRecentLiveUpdates,
} = useSessionsPage({
  range: 'recent3d',
  page: 1,
  pageSize: 12,
  sort: 'updated_at desc',
});
const {
  rows: historyRows,
  loadSessions: loadHistorySessions,
  startLiveUpdates: startHistoryLiveUpdates,
  stopLiveUpdates: stopHistoryLiveUpdates,
} = useSessionsPage({
  range: 'history7d',
  page: 1,
  pageSize: 8,
  sort: 'updated_at desc',
});
const { projects, loadProjects } = useProjects();

const recentCards = computed(() => recentRows.value);
const historyCards = computed(() => historyRows.value);
const visibleCards = computed(() => [...recentRows.value, ...historyRows.value]);
const answerDialog = ref(false);
const activeQuestionSessionId = ref('');
const pendingQuestionBatches = ref<QuestionBatch[]>([]);
const questionsLoading = ref(false);
const questionsSubmitting = ref(false);

const stats = computed(() => [
  {
    label: '运行中卡片',
    value: visibleCards.value.filter((item) => item.status === 'running').length,
    icon: 'play_circle',
    color: 'positive',
  },
  {
    label: '待回答',
    value: visibleCards.value.filter((item) => item.pendingQuestion).length,
    icon: 'help',
    color: 'warning',
  },
  { label: '项目', value: projects.value.length, icon: 'folder_open', color: 'primary' },
]);

onMounted(() => {
  void loadProjects();
  void loadRecentSessions();
  void loadHistorySessions();
  startRecentLiveUpdates();
  startHistoryLiveUpdates();
});

onUnmounted(() => {
  stopRecentLiveUpdates();
  stopHistoryLiveUpdates();
});

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function statusColor(status: SessionStatus) {
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
  return colors[status];
}

function statusIcon(status: SessionStatus) {
  const icons: Record<SessionStatus, string> = {
    created: 'radio_button_unchecked',
    starting: 'pending',
    running: 'play_arrow',
    waiting_user: 'help',
    waiting_approval: 'fact_check',
    stopping: 'pause_circle',
    stopped: 'stop_circle',
    resume_failed: 'sync_problem',
    failed: 'error',
    blocked: 'error',
    completed: 'check_circle',
    closed: 'archive',
  };
  return icons[status];
}

function statusLabel(status: SessionStatus) {
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
  return labels[status];
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
