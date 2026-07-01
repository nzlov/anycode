<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">会话表格</div>
        <div class="text-body2 text-muted">分页、过滤和排序后续由 GraphQL 后端计算</div>
      </div>
    </div>

    <q-card flat bordered class="table-card">
      <q-card-section class="table-toolbar">
        <q-input v-model="filter" dense outlined debounce="200" placeholder="搜索需求、项目或分支">
          <template #prepend>
            <q-icon name="search" />
          </template>
        </q-input>
        <q-select v-model="status" dense outlined emit-value map-options :options="statusOptions" />
      </q-card-section>

      <q-table
        flat
        :rows="filteredRows"
        :columns="columns"
        row-key="id"
        :pagination="{ rowsPerPage: 8 }"
        class="session-table"
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
              :to="`/sessions/${props.row.id}`"
            />
            <q-btn flat round dense icon="difference" color="secondary" to="/diff" />
          </q-td>
        </template>
      </q-table>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import { getProjectName, sessions, type SessionStatus } from '@/mocks/workbench';

const filter = ref('');
const status = ref<SessionStatus | 'all'>('all');

const statusOptions = [
  { label: '全部状态', value: 'all' },
  { label: '运行中', value: 'running' },
  { label: '待回答', value: 'waiting_user' },
  { label: '已停止', value: 'stopped' },
  { label: '阻塞', value: 'blocked' },
  { label: '已完成', value: 'completed' },
];

const columns = [
  { name: 'title', label: '需求', field: 'title', align: 'left' as const, sortable: true },
  {
    name: 'project',
    label: '项目',
    field: (row: (typeof sessions)[number]) => getProjectName(row.projectId),
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

const filteredRows = computed(() => {
  const needle = filter.value.trim().toLowerCase();
  return sessions.filter((session) => {
    const matchesStatus = status.value === 'all' || session.status === status.value;
    const matchesFilter =
      needle.length === 0 ||
      [session.title, session.summary, session.branch, getProjectName(session.projectId)].some(
        (value) => value.toLowerCase().includes(needle),
      );
    return matchesStatus && matchesFilter;
  });
});

function statusColor(value: SessionStatus) {
  const colors: Record<SessionStatus, string> = {
    running: 'positive',
    waiting_user: 'warning',
    stopped: 'blue-grey',
    blocked: 'negative',
    completed: 'primary',
  };
  return colors[value];
}

function statusLabel(value: SessionStatus) {
  const labels: Record<SessionStatus, string> = {
    running: '运行中',
    waiting_user: '待回答',
    stopped: '已停止',
    blocked: '阻塞',
    completed: '已完成',
  };
  return labels[value];
}
</script>
