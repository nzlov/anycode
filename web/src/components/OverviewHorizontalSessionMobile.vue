<template>
  <article class="overview-horizontal-session-mobile" :aria-label="`${card.title} 会话详情`">
    <header class="overview-horizontal-session-mobile__header">
      <div class="overview-horizontal-session-mobile__heading">
        <div class="overview-horizontal-session-mobile__badges">
          <q-badge outline :color="statusColor(card.status)" :label="statusLabel(card.status)" />
          <q-badge rounded class="lane-mode-chip" :label="modeLabel" />
        </div>
        <div class="overview-horizontal-session-mobile__title" :title="card.title">
          {{ card.title }}
        </div>
        <div class="overview-horizontal-session-mobile__project" :title="card.projectName">
          {{ card.projectName }}
        </div>
      </div>
      <q-btn
        flat
        round
        dense
        class="app-icon-btn"
        icon="open_in_new"
        aria-label="打开会话详情"
        :to="{ name: 'session-detail', params: { id: card.id } }"
      >
        <q-tooltip>打开会话详情</q-tooltip>
      </q-btn>
    </header>
    <SessionDetailView
      class="overview-horizontal-session-mobile__detail"
      :session-id="card.id"
      layout="mobile"
    />
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import SessionDetailView from '@/components/SessionDetailView.vue';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import type { SessionCard } from '@/services/sessions';

const props = defineProps<{
  card: SessionCard;
}>();

const modeLabel = computed(() => (props.card.mode === 'workflow' ? '流程' : '对话'));
</script>

<style scoped>
.overview-horizontal-session-mobile {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--ac-surface);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
}

.overview-horizontal-session-mobile__header {
  display: flex;
  min-width: 0;
  min-height: 72px;
  flex: 0 0 auto;
  align-items: flex-start;
  justify-content: space-between;
  gap: 8px;
  padding: 8px 6px 8px 10px;
  border-bottom: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.overview-horizontal-session-mobile__heading {
  display: grid;
  min-width: 0;
  gap: 4px;
}

.overview-horizontal-session-mobile__badges {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 6px;
}

.overview-horizontal-session-mobile__title,
.overview-horizontal-session-mobile__project {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-session-mobile__title {
  color: var(--ac-text);
  font-size: 14px;
  font-weight: 700;
}

.overview-horizontal-session-mobile__project {
  color: var(--ac-text-muted);
  font-size: 12px;
}

.overview-horizontal-session-mobile__detail {
  min-height: 0;
  flex: 1 1 auto;
}
</style>
