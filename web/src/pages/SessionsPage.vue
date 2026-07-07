<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">会话表格</div>
        <div class="text-body2 text-muted">分页、过滤、排序和 total 均由 GraphQL 后端计算</div>
      </div>
    </div>

    <q-card flat bordered class="table-filter-card">
      <q-card-section class="table-toolbar">
        <div class="text-subtitle2 text-weight-bold">过滤条件</div>
        <q-input v-model="filter" dense outlined debounce="200" placeholder="搜索需求、项目或分支">
          <template #prepend>
            <q-icon name="search" />
          </template>
        </q-input>
        <q-select v-model="status" dense outlined emit-value map-options :options="statusOptions" />
      </q-card-section>
    </q-card>

    <q-card flat bordered class="table-card q-mt-md">
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
            <div class="text-caption text-muted">{{ props.row.summary }}</div>
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

        <template #body-cell-actions="props">
          <q-td :props="props">
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
              color="secondary"
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute } from 'vue-router';

import { useSessionsPage } from '@/composables/useSessionsPage';
import type { SessionCard, SessionStatus } from '@/services/sessions';

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
  startLiveUpdates,
  stopLiveUpdates,
} = useSessionsPage({
  page: 1,
  pageSize: 8,
  sort: 'updated_at desc',
});
const status = ref<SessionStatus | 'all'>('all');

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '运行中', value: 'running' },
  { label: '待回答', value: 'waiting_user' },
  { label: '待审批', value: 'waiting_approval' },
  { label: '已停止', value: 'stopped' },
  { label: '阻塞', value: 'blocked' },
  { label: '已完成', value: 'completed' },
];

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
    : ['title', 'project', 'branch', 'node', 'updatedAt', 'status', 'actions'],
);

watch(status, (value) => {
  scope.value = value === 'all' ? '' : value;
  page.value = 1;
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
  startLiveUpdates();
});

onUnmounted(() => {
  stopLiveUpdates();
});

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
