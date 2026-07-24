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
        <q-badge
          v-if="card.mode !== 'terminal'"
          rounded
          class="lane-mode-chip"
          :label="modeBadgeLabel(card.mode)"
        />
        <q-btn
          v-if="canStopTerminal"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="negative"
          icon="stop"
          aria-label="停止 Terminal"
          :loading="terminalAction === 'stop'"
          @click="stopTerminal"
        >
          <q-tooltip>停止 Terminal</q-tooltip>
        </q-btn>
        <q-btn
          v-if="canStartTerminal"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="primary"
          icon="play_arrow"
          aria-label="启动 Terminal"
          :loading="terminalAction === 'start'"
          @click="startTerminal"
        >
          <q-tooltip>启动 Terminal</q-tooltip>
        </q-btn>
        <q-btn
          v-if="canCloseTerminal"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="negative"
          icon="close"
          aria-label="关闭 Terminal 卡片"
          :loading="terminalAction === 'close'"
          @click="closeTerminal"
        >
          <q-tooltip>关闭 Terminal 卡片</q-tooltip>
        </q-btn>
        <SessionTerminalButton v-if="card.mode !== 'terminal'" :source-session-id="card.id" />
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
    <TerminalView
      v-if="card.mode === 'terminal'"
      :key="`${card.id}:${card.status === 'running' ? 'running' : 'stopped'}`"
      class="overview-horizontal-session-desktop__detail"
      :session-id="card.id"
      :interactive="card.status === 'running'"
    />
    <SessionDetailView
      v-else
      class="overview-horizontal-session-desktop__detail"
      :session-id="card.id"
      layout="desktop"
    />
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import SessionDetailView from '@/components/SessionDetailView.vue';
import SessionPriorityControl from '@/components/SessionPriorityControl.vue';
import SessionTerminalButton from '@/components/SessionTerminalButton.vue';
import TerminalView from '@/components/TerminalView.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import { sessionModeBadgeLabel as modeBadgeLabel } from '@/services/sessionModePresentation';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import {
  closeSession,
  executeSession,
  stopSession,
  type SessionCard,
  type SessionPriority,
} from '@/services/sessions';

const props = defineProps<{
  card: SessionCard;
  priorityLoading?: boolean;
}>();

const emit = defineEmits<{
  'set-priority': [priority: SessionPriority];
}>();

const terminalAction = ref<'start' | 'stop' | 'close' | ''>('');
const canStopTerminal = computed(
  () => props.card.mode === 'terminal' && props.card.availableActions.includes('stop'),
);
const canStartTerminal = computed(
  () => props.card.mode === 'terminal' && props.card.availableActions.includes('execute'),
);
const canCloseTerminal = computed(
  () => props.card.mode === 'terminal' && props.card.availableActions.includes('close'),
);

async function startTerminal() {
  if (!canStartTerminal.value || terminalAction.value) return;
  terminalAction.value = 'start';
  try {
    await executeSession(props.card.id);
  } finally {
    terminalAction.value = '';
  }
}

async function stopTerminal() {
  if (!canStopTerminal.value || terminalAction.value) return;
  terminalAction.value = 'stop';
  try {
    await stopSession(props.card.id);
  } finally {
    terminalAction.value = '';
  }
}

async function closeTerminal() {
  if (!canCloseTerminal.value || terminalAction.value) return;
  terminalAction.value = 'close';
  try {
    await closeSession(props.card.id);
  } finally {
    terminalAction.value = '';
  }
}
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
