<template>
  <article class="overview-horizontal-session-desktop" :aria-label="`${card.title} 会话详情`">
    <header class="overview-horizontal-session-desktop__header">
      <div class="overview-horizontal-session-desktop__identity">
        <div class="overview-horizontal-session-desktop__title" :title="card.title">
          {{ card.title }}
        </div>
        <div class="overview-horizontal-session-desktop__meta">
          <span :title="card.projectName">{{ card.projectName }}</span>
          <TokenUsageDisplay v-if="card.usage" :usage="card.usage" />
          <span :title="card.branch">{{ card.branch }}</span>
          <span v-if="card.mode === 'workflow'" :title="card.node">{{ card.node }}</span>
        </div>
      </div>
      <div class="overview-horizontal-session-desktop__actions">
        <SessionPriorityControl
          :priority="card.priority"
          :loading="priorityLoading"
          :disabled="card.status === 'closed'"
          @change="emit('set-priority', $event)"
        />
        <q-badge outline :color="statusColor(card.status)" :label="statusLabel(card.status)" />
        <q-badge rounded class="lane-mode-chip" :label="modeBadgeLabel(card.mode)" />
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
      </div>
    </header>
    <SessionDetailView
      class="overview-horizontal-session-desktop__detail"
      :session-id="card.id"
      layout="desktop"
    />
  </article>
</template>

<script setup lang="ts">
import SessionDetailView from '@/components/SessionDetailView.vue';
import SessionPriorityControl from '@/components/SessionPriorityControl.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import { sessionModeBadgeLabel as modeBadgeLabel } from '@/services/sessionModePresentation';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import type { SessionCard, SessionPriority } from '@/services/sessions';

defineProps<{
  card: SessionCard;
  priorityLoading?: boolean;
}>();

const emit = defineEmits<{
  'set-priority': [priority: SessionPriority];
}>();

</script>

<style scoped>
.overview-horizontal-session-desktop {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--ac-surface);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
}

.overview-horizontal-session-desktop__header {
  display: flex;
  min-width: 0;
  min-height: 72px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 10px 10px 10px 14px;
  border-bottom: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.overview-horizontal-session-desktop__identity {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.overview-horizontal-session-desktop__title {
  overflow: hidden;
  color: var(--ac-text);
  font-size: 16px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-session-desktop__meta,
.overview-horizontal-session-desktop__actions {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.overview-horizontal-session-desktop__meta {
  color: var(--ac-text-muted);
  font-size: 12px;
}

.overview-horizontal-session-desktop__meta span {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-session-desktop__actions {
  flex: 0 0 auto;
}

.overview-horizontal-session-desktop__detail {
  min-height: 0;
  flex: 1 1 auto;
}
</style>
