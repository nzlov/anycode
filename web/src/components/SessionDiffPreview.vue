<template>
  <div class="session-diff-preview">
    <div v-if="loading" class="session-diff-preview__loading">
      <q-spinner color="primary" size="28px" />
      <span>正在加载 Diff</span>
    </div>
    <q-banner v-else-if="error" dense rounded class="state-banner bg-negative text-white">
      {{ error }}
    </q-banner>
    <q-banner v-else-if="!available" dense rounded class="state-banner bg-warning text-dark">
      当前会话没有可用 Diff
    </q-banner>
    <q-banner v-else-if="total === 0" dense rounded class="state-banner bg-grey-2 text-dark">
      当前会话没有变更
    </q-banner>
    <template v-else>
      <q-banner v-if="truncated" dense rounded class="state-banner bg-warning text-dark q-mb-sm">
        当前只展示前 {{ fileDiffs.length }} 个文件
        <q-btn flat dense no-caps label="完整 Diff" :to="fullDiffRoute" />
      </q-banner>
      <DiffViewer :file-diffs="fileDiffs" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { RouteLocationRaw } from 'vue-router';

import DiffViewer from '@/components/DiffViewer.vue';
import type { FileDiff } from '@/services/diff';

const props = defineProps<{
  loading: boolean;
  error: string;
  available: boolean;
  fileDiffs: FileDiff[];
  total: number;
  fullDiffRoute: RouteLocationRaw;
}>();

const truncated = computed(() => props.total > props.fileDiffs.length);
</script>

<style scoped>
.session-diff-preview {
  min-width: 0;
}

.session-diff-preview__loading {
  min-height: 160px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--ac-text-muted);
}
</style>
