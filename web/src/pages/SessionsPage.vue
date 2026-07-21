<template>
  <q-page class="page-shell">
    <PageToolbar title="会话表格" compact-title-on-mobile>
      <q-input
        v-model="filter"
        dense
        outlined
        debounce="200"
        class="sessions-toolbar__search"
        placeholder="搜索需求、项目或分支"
        aria-label="搜索会话"
      >
        <template #prepend>
          <q-icon name="search" />
        </template>
      </q-input>
      <q-select
        v-model="status"
        dense
        outlined
        emit-value
        map-options
        class="sessions-toolbar__status"
        :options="statusOptions"
        aria-label="按状态筛选会话"
      />
    </PageToolbar>

    <q-card flat bordered class="table-card">
      <q-table
        flat
        wrap-cells
        :rows="rows"
        :columns="columns"
        :visible-columns="visibleColumns"
        row-key="id"
        :loading="loading"
        v-model:pagination="pagination"
        :rows-per-page-options="[8, 20, 50]"
        binary-state-sort
        class="session-table"
        @request="onTableRequest"
      >
        <template #body-cell-title="props">
          <q-td :props="props">
            <router-link class="table-link" :to="`/sessions/${props.row.id}`">
              {{ props.row.title }}
            </router-link>
          </q-td>
        </template>

        <template #body-cell-status="props">
          <q-td :props="props">
            <q-badge
              outline
              :color="statusColor(props.row.status)"
              :label="statusLabel(props.row.status)"
            />
          </q-td>
        </template>

        <template #body-cell-node="props">
          <q-td :props="props">
            {{ props.row.mode === 'workflow' ? props.row.node : '-' }}
          </q-td>
        </template>

        <template #body-cell-diff="props">
          <q-td :props="props" class="session-table__diff-count">
            {{ props.row.filesChanged }}
          </q-td>
        </template>

        <template #body-cell-tokens="props">
          <q-td :props="props">
            <TokenUsageDisplay v-if="props.row.usage" :usage="props.row.usage" />
            <span v-else>-</span>
          </q-td>
        </template>

        <template #body-cell-actions="props">
          <q-td :props="props">
            <q-btn
              v-if="props.row.status === 'queued' && props.row.availableActions.includes('stop')"
              flat
              round
              dense
              icon="cancel"
              color="negative"
              aria-label="取消排队"
              :loading="cancellingSessionId === props.row.id"
              @click="cancelQueuedSession(props.row)"
            >
              <q-tooltip>取消排队</q-tooltip>
            </q-btn>
            <q-btn
              flat
              round
              dense
              icon="open_in_new"
              color="primary"
              aria-label="打开卡片"
              :to="`/sessions/${props.row.id}`"
            >
              <q-tooltip>打开卡片详情</q-tooltip>
            </q-btn>
            <q-btn
              flat
              round
              dense
              icon="difference"
              color="primary"
              aria-label="查看 Diff"
              :to="{ path: '/diff', query: { sessionId: props.row.id, mode: 'all' } }"
            >
              <q-tooltip>查看完整 Diff</q-tooltip>
            </q-btn>
          </q-td>
        </template>
      </q-table>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute } from 'vue-router';

import PageToolbar from '@/components/PageToolbar.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import { useSessionsPage } from '@/composables/useSessionsPage';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import { stopSession, type SessionCard, type SessionStatus } from '@/services/sessions';

const $q = useQuasar();
const route = useRoute();
const projectScopeId = computed(() => {
  const value = route.query.projectId;
  return typeof value === 'string' ? value : '';
});
const {
  rows,
  pageInfo,
  loading,
  filter,
  scope,
  projectId,
  page,
  pageSize,
  sort,
  loadSessions,
} = useSessionsPage({
  page: 1,
  pageSize: 8,
  sort: 'updated_at desc',
});

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '运行中', value: 'running' },
  { label: '待回答', value: 'waiting_user' },
  { label: '待审批', value: 'waiting_approval' },
  { label: '已停止', value: 'stopped' },
  { label: '阻塞', value: 'blocked' },
  { label: '已完成', value: 'completed' },
  { label: '已关闭', value: 'closed' },
];
const statusValues = new Set<SessionStatus>([
  'running',
  'waiting_user',
  'waiting_approval',
  'stopped',
  'blocked',
  'completed',
  'closed',
]);
const routeStatus = computed<SessionStatus | 'all'>(() => {
  const value = route.query.scope;
  return typeof value === 'string' && statusValues.has(value as SessionStatus)
    ? (value as SessionStatus)
    : 'all';
});
const status = ref<SessionStatus | 'all'>(routeStatus.value);
const cancellingSessionId = ref('');
scope.value = status.value === 'all' ? '' : status.value;

const columns = [
  { name: 'title', label: '需求', field: 'title', align: 'left' as const, sortable: true },
  {
    name: 'project',
    label: '项目',
    field: (row: SessionCard) => row.projectName,
    align: 'left' as const,
    sortable: true,
  },
  { name: 'branch', label: '分支', field: 'branch', align: 'left' as const, sortable: true },
  { name: 'node', label: '当前节点', field: 'node', align: 'left' as const },
  {
    name: 'diff',
    label: 'Diff',
    field: (row: SessionCard) => row.filesChanged,
    align: 'left' as const,
  },
  {
    name: 'tokens',
    label: 'Token 用量',
    field: (row: SessionCard) => row.usage?.totalTokens ?? 0,
    align: 'left' as const,
  },
  {
    name: 'updatedAt',
    label: '更新时间',
    field: 'updatedAt',
    align: 'left' as const,
    sortable: true,
  },
  { name: 'status', label: '状态', field: 'status', align: 'left' as const },
  { name: 'actions', label: '', field: 'actions', align: 'right' as const },
];

const pagination = computed({
  get() {
    return {
      page: pageInfo.value.page,
      rowsPerPage: pageInfo.value.pageSize,
      rowsNumber: pageInfo.value.total,
      sortBy: tableSortBy(sort.value),
      descending: isDescending(sort.value),
    };
  },
  set(value: {
    page?: number;
    rowsPerPage?: number;
    sortBy?: string | null;
    descending?: boolean;
  }) {
    page.value = value.page ?? page.value;
    pageSize.value = value.rowsPerPage ?? pageSize.value;
    sort.value = sortValue(value.sortBy, value.descending ?? true);
  },
});
const visibleColumns = computed(() =>
  $q.screen.lt.sm
    ? ['title', 'status', 'actions']
    : $q.screen.lt.md
      ? ['title', 'diff', 'tokens', 'updatedAt', 'status', 'actions']
      : ['title', 'project', 'branch', 'node', 'diff', 'tokens', 'updatedAt', 'status', 'actions'],
);

watch(status, (value) => {
  scope.value = value === 'all' ? '' : value;
  page.value = 1;
});

watch(routeStatus, (value) => {
  status.value = value;
});

watch([filter, scope], () => {
  page.value = 1;
  void loadSessions();
});

watch(projectScopeId, (value) => {
  projectId.value = value;
  page.value = 1;
  void loadSessions();
});

onMounted(() => {
  projectId.value = projectScopeId.value;
  void loadSessions();
});

function onTableRequest(props: {
  pagination: {
    page: number;
    rowsPerPage: number;
    sortBy?: string | null;
    descending?: boolean;
  };
}) {
  pagination.value = props.pagination;
  void loadSessions();
}

async function cancelQueuedSession(session: SessionCard) {
  if (session.status !== 'queued' || !session.availableActions.includes('stop')) return;
  cancellingSessionId.value = session.id;
  try {
    await stopSession(session.id);
    await loadSessions();
  } catch {
    await loadSessions().catch(() => undefined);
  } finally {
    cancellingSessionId.value = '';
  }
}

function sortValue(sortBy?: string | null, descending = true) {
  const field = sortField(sortBy);
  return `${field} ${descending ? 'desc' : 'asc'}`;
}

function sortField(sortBy?: string | null) {
  if (sortBy === 'title') return 'requirement';
  if (sortBy === 'project') return 'project_id';
  if (sortBy === 'branch') return 'base_branch';
  if (sortBy === 'status') return 'status';
  if (sortBy === 'updatedAt') return 'updated_at';
  return 'updated_at';
}

function tableSortBy(value: string) {
  const field = value.trim().split(/\s+/)[0] ?? '';
  if (field === 'requirement') return 'title';
  if (field === 'project_id') return 'project';
  if (field === 'base_branch') return 'branch';
  if (field === 'status') return 'status';
  return 'updatedAt';
}

function isDescending(value: string) {
  return !/\sasc$/i.test(value.trim()) && !value.trim().startsWith('+');
}
</script>
