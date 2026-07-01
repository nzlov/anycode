<template>
  <q-page class="workbench-page page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">总揽</div>
        <div class="text-body2 text-muted">最近 3 天运行卡片与近 7 天历史记录</div>
      </div>
      <q-btn
        flat
        color="primary"
        icon-right="chevron_right"
        label="更多历史"
        no-caps
        to="/sessions"
      />
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
                    <span>{{ getProjectName(card.projectId) }}</span>
                    <q-icon name="account_tree" />
                    <span>{{ card.node }}</span>
                    <q-icon name="difference" />
                    <span>{{ card.filesChanged }} 个文件</span>
                  </div>
                </div>
                <q-btn flat round color="primary" icon="chevron_right" aria-label="打开卡片" />
              </div>
            </q-card-section>
          </q-card>
        </div>
      </section>

      <section class="col-12 col-lg-5">
        <div class="section-heading">
          <div class="text-subtitle1 text-weight-bold">历史</div>
          <q-chip dense outline color="secondary" icon="history">近 7 天</q-chip>
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
                >{{ getProjectName(item.projectId) }} · {{ item.updatedAt }}</q-item-label
              >
            </q-item-section>
            <q-item-section side>
              <q-badge
                outline
                :color="item.pendingQuestion ? 'warning' : 'blue-grey'"
                :label="item.pendingQuestion ? '待回答' : '已同步'"
              />
            </q-item-section>
          </q-item>
        </q-list>
      </section>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue';

import { useSessionsPage } from '@/composables/useSessionsPage';
import { getProjectName, projects, type SessionMode, type SessionStatus } from '@/mocks/workbench';

const { rows, loadSessions } = useSessionsPage({
  scope: 'overview',
  range: 'all',
  page: 1,
  pageSize: 8,
  sort: 'updated_at desc',
});

const recentCards = computed(() => rows.value.slice(0, 2));
const historyCards = computed(() => rows.value.slice(2));

const stats = computed(() => [
  {
    label: '运行中卡片',
    value: rows.value.filter((item) => item.status === 'running').length,
    icon: 'play_circle',
    color: 'positive',
  },
  {
    label: '待回答',
    value: rows.value.filter((item) => item.pendingQuestion).length,
    icon: 'help',
    color: 'warning',
  },
  { label: '项目', value: projects.length, icon: 'folder_open', color: 'primary' },
]);

onMounted(() => {
  void loadSessions();
});

function modeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程模式' : '会话模式';
}

function statusColor(status: SessionStatus) {
  const colors: Record<SessionStatus, string> = {
    running: 'positive',
    waiting_user: 'warning',
    stopped: 'blue-grey',
    blocked: 'negative',
    completed: 'primary',
  };
  return colors[status];
}

function statusIcon(status: SessionStatus) {
  const icons: Record<SessionStatus, string> = {
    running: 'play_arrow',
    waiting_user: 'help',
    stopped: 'stop_circle',
    blocked: 'error',
    completed: 'check_circle',
  };
  return icons[status];
}
</script>
