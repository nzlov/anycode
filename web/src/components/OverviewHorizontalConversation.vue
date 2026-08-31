<template>
  <article
    class="overview-horizontal-conversation"
    :class="`overview-horizontal-conversation--${layout}`"
    :aria-label="`${card.title} 会话详情`"
  >
    <header class="overview-horizontal-conversation__header">
      <div class="overview-horizontal-conversation__identity">
        <div class="overview-horizontal-conversation__title" :title="card.title">
          {{ card.title }}
        </div>
        <div class="overview-horizontal-conversation__meta">
          <span :title="card.projectName">{{ card.projectName }}</span>
          <TokenUsageDisplay v-if="card.usage" :usage="card.usage" />
          <span v-if="layout === 'desktop' && card.projectIsGit" :title="card.branch">
            {{ card.branch }}
          </span>
          <span v-if="layout === 'desktop' && card.mode === 'workflow'" :title="card.node">
            {{ card.node }}
          </span>
        </div>
      </div>
      <div class="overview-horizontal-conversation__badges">
        <SessionPriorityControl
          :priority="card.priority"
          :loading="priorityLoading"
          :disabled="card.status === 'closed'"
          @change="emit('set-priority', $event)"
        />
        <q-badge outline :color="statusColor(card.status)" :label="statusLabel(card.status)" />
        <q-badge rounded class="lane-mode-chip" :label="modeBadgeLabel(card.mode)" />
      </div>
      <div class="overview-horizontal-conversation__actions">
        <SessionTunnelButton :tunnels="tunnels" />
        <SessionTerminalButton
          :source-session-id="card.id"
          stay-on-page
          @opened="emit('terminal-opened', $event)"
        />
        <SessionForkButton
          v-if="card.availableActions.includes('fork')"
          :source-session-id="card.id"
          :project-is-git="card.projectIsGit"
          stay-on-page
          @forked="emit('forked', $event)"
        />
        <q-btn
          v-if="mindMapUpdated"
          flat
          round
          dense
          class="app-icon-btn"
          icon="hub"
          aria-label="打开思维图"
          :to="{
            name: 'project-mind-map',
            params: { projectId: card.projectId },
            query: { card: card.id },
          }"
        >
          <q-tooltip>打开思维图</q-tooltip>
        </q-btn>
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
        <q-btn
          v-if="mindMapRealtime && card.availableActions.includes('close')"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="positive"
          icon="merge"
          aria-label="合并思维图并关闭"
          :loading="closeLoading"
          @click="emit('merge-close')"
        >
          <q-tooltip>合并思维图并关闭</q-tooltip>
        </q-btn>
        <q-btn
          v-if="card.availableActions.includes('close')"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="negative"
          icon="close"
          :aria-label="mindMapRealtime ? '关闭，不合并思维图' : '关闭卡片'"
          :loading="closeLoading"
          @click="emit('close')"
        >
          <q-tooltip>{{ mindMapRealtime ? '关闭，不合并思维图' : '关闭卡片' }}</q-tooltip>
        </q-btn>
      </div>
    </header>
    <SessionDetailView
      class="overview-horizontal-conversation__detail"
      :session-id="card.id"
      :layout="layout"
      :mind-map-realtime="mindMapRealtime"
    />
  </article>
</template>

<script setup lang="ts">
import SessionDetailView from '@/components/SessionDetailView.vue';
import SessionForkButton from '@/components/SessionForkButton.vue';
import SessionPriorityControl from '@/components/SessionPriorityControl.vue';
import SessionTerminalButton from '@/components/SessionTerminalButton.vue';
import SessionTunnelButton from '@/components/SessionTunnelButton.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import { sessionModeBadgeLabel as modeBadgeLabel } from '@/services/sessionModePresentation';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import type { SessionCard, SessionPriority } from '@/services/sessions';
import type { Tunnel } from '@/services/tunnels';

defineProps<{
  card: SessionCard;
  tunnels: Tunnel[];
  layout: 'mobile' | 'desktop';
  priorityLoading?: boolean;
  closeLoading?: boolean;
  mindMapRealtime?: boolean;
  mindMapUpdated?: boolean;
}>();

const emit = defineEmits<{
  'set-priority': [priority: SessionPriority];
  'terminal-opened': [sessionId: string];
  forked: [sessionId: string];
  close: [];
  'merge-close': [];
}>();
</script>

<style scoped>
.overview-horizontal-conversation {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--ac-surface);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
}

.overview-horizontal-conversation__header {
  display: grid;
  min-width: 0;
  min-height: 72px;
  flex: 0 0 auto;
  gap: 8px;
  border-bottom: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.overview-horizontal-conversation--mobile .overview-horizontal-conversation__header {
  grid-template-areas:
    'badges actions'
    'identity identity';
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  padding: 8px 6px 8px 10px;
}

.overview-horizontal-conversation--desktop .overview-horizontal-conversation__header {
  grid-template-areas: 'identity badges actions';
  grid-template-columns: minmax(0, 1fr) auto auto;
  align-items: center;
  gap: 16px;
  padding: 10px 10px 10px 14px;
}

.overview-horizontal-conversation__identity {
  display: grid;
  min-width: 0;
  grid-area: identity;
  gap: 4px;
}

.overview-horizontal-conversation--desktop .overview-horizontal-conversation__identity {
  gap: 6px;
}

.overview-horizontal-conversation__title,
.overview-horizontal-conversation__meta > span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-conversation__title {
  color: var(--ac-text);
  font-size: 14px;
  font-weight: 700;
}

.overview-horizontal-conversation--desktop .overview-horizontal-conversation__title {
  font-size: 16px;
}

.overview-horizontal-conversation__meta,
.overview-horizontal-conversation__badges,
.overview-horizontal-conversation__actions {
  display: flex;
  min-width: 0;
  align-items: center;
}

.overview-horizontal-conversation__meta {
  gap: 8px;
  color: var(--ac-text-muted);
  font-size: 12px;
}

.overview-horizontal-conversation__meta > span {
  max-width: 180px;
}

.overview-horizontal-conversation__badges {
  flex-wrap: wrap;
  grid-area: badges;
  gap: 6px;
}

.overview-horizontal-conversation__actions {
  flex: 0 0 auto;
  grid-area: actions;
}

.overview-horizontal-conversation__detail {
  min-height: 0;
  flex: 1 1 auto;
}
</style>
