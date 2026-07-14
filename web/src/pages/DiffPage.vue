<template>
  <q-page class="page-shell diff-page">
    <div class="page-heading">
      <div class="text-h5 text-weight-bold">当前分支变更</div>
    </div>

    <q-banner v-if="!target" rounded class="state-banner bg-warning text-dark">
      请从会话详情进入 Diff 页面，或在地址 query 中提供 projectId 与 branch。
    </q-banner>

    <DiffWorkspace v-else v-model="workspaceState" :target="target" />
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import DiffWorkspace from '@/components/DiffWorkspace.vue';
import type { DiffMode, DiffWorkspaceState, DiffWorkspaceTarget } from '@/services/diff';

const route = useRoute();
const router = useRouter();
const target = computed<DiffWorkspaceTarget | null>(() => {
  const projectId = stringQuery(route.query.projectId);
  const branch = stringQuery(route.query.branch);
  if (projectId && branch) return { kind: 'branch', projectId, branch };
  const sessionId = stringQuery(route.query.sessionId);
  return sessionId ? { kind: 'session', sessionId } : null;
});
const workspaceState = ref(readWorkspaceState());

function readWorkspaceState(): DiffWorkspaceState {
  return {
    mode: normalizeMode(stringQuery(route.query.mode)),
    filePath: stringQuery(route.query.filePath),
    page: positiveIntQuery(route.query.page, 1),
    pageSize: positiveIntQuery(route.query.pageSize, 20),
  };
}

function stringQuery(value: unknown) {
  if (Array.isArray(value)) return String(value[0] ?? '');
  return typeof value === 'string' ? value : '';
}

function positiveIntQuery(value: unknown, fallback: number) {
  const parsed = Number.parseInt(stringQuery(value), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function normalizeMode(mode: string): DiffMode {
  return mode === 'all' ? 'all' : 'single';
}

function sameWorkspaceState(left: DiffWorkspaceState, right: DiffWorkspaceState) {
  return (
    left.mode === right.mode &&
    left.filePath === right.filePath &&
    left.page === right.page &&
    left.pageSize === right.pageSize
  );
}

function syncRouteQuery(state: DiffWorkspaceState) {
  if (!target.value) return;
  const nextQuery = {
    ...route.query,
    sessionId: target.value.kind === 'session' ? target.value.sessionId : undefined,
    projectId: target.value.kind === 'branch' ? target.value.projectId : undefined,
    branch: target.value.kind === 'branch' ? target.value.branch : undefined,
    mode: state.mode,
    filePath: state.mode === 'single' && state.filePath ? state.filePath : undefined,
    page: String(state.page),
    pageSize: String(state.pageSize),
  };
  if (JSON.stringify(route.query) !== JSON.stringify(nextQuery)) {
    void router.replace({ query: nextQuery });
  }
}

watch(
  workspaceState,
  (state) => {
    syncRouteQuery(state);
  },
  { deep: true },
);

watch(
  () => [
    route.query.sessionId,
    route.query.projectId,
    route.query.branch,
    route.query.mode,
    route.query.filePath,
    route.query.page,
    route.query.pageSize,
  ],
  () => {
    const next = readWorkspaceState();
    if (!sameWorkspaceState(workspaceState.value, next)) workspaceState.value = next;
  },
);
</script>

<style scoped>
.diff-page,
.heading-copy {
  min-width: 0;
}

.state-banner {
  margin-bottom: 16px;
}
</style>
