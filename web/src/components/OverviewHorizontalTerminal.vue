<template>
  <article class="overview-horizontal-terminal" :aria-label="`${card.title} Terminal 详情`">
    <header class="overview-horizontal-terminal__header">
      <div class="overview-horizontal-terminal__identity">
        <div class="overview-horizontal-terminal__title" :title="card.title">
          {{ card.title }}
        </div>
        <div class="overview-horizontal-terminal__meta">
          <span :title="card.projectName">{{ card.projectName }}</span>
          <TokenUsageDisplay v-if="card.usage" :usage="card.usage" />
          <span :title="card.branch">{{ card.branch }}</span>
        </div>
      </div>
      <div class="overview-horizontal-terminal__actions">
        <SessionTunnelButton :tunnels="tunnels" />
        <SessionPriorityControl
          :priority="card.priority"
          :loading="priorityLoading"
          :disabled="card.status === 'closed'"
          @change="emit('set-priority', $event)"
        />
        <q-badge outline :color="statusColor(card.status)" :label="statusLabel(card.status)" />
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
          v-if="canClose"
          flat
          dense
          class="lane-icon-btn app-icon-btn"
          color="negative"
          icon="close"
          aria-label="关闭卡片"
          :loading="closeLoading"
          @click="emit('close')"
        >
          <q-tooltip>关闭卡片</q-tooltip>
        </q-btn>
      </div>
    </header>
    <TerminalView
      :key="`${card.id}:${card.status === 'running' ? 'running' : 'stopped'}`"
      class="overview-horizontal-terminal__detail"
      :session-id="card.id"
      :interactive="card.status === 'running'"
      :resize-paused="terminalResizePaused"
    />
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import SessionPriorityControl from '@/components/SessionPriorityControl.vue';
import SessionTunnelButton from '@/components/SessionTunnelButton.vue';
import TerminalView from '@/components/TerminalView.vue';
import TokenUsageDisplay from '@/components/TokenUsageDisplay.vue';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';
import {
  executeSession,
  stopSession,
  type SessionCard,
  type SessionPriority,
} from '@/services/sessions';
import type { Tunnel } from '@/services/tunnels';

const props = defineProps<{
  card: SessionCard;
  tunnels: Tunnel[];
  priorityLoading?: boolean;
  closeLoading?: boolean;
  terminalResizePaused?: boolean;
}>();

const emit = defineEmits<{
  'set-priority': [priority: SessionPriority];
  close: [];
}>();

const terminalAction = ref<'start' | 'stop' | ''>('');
const canStopTerminal = computed(
  () => props.card.mode === 'terminal' && props.card.availableActions.includes('stop'),
);
const canStartTerminal = computed(
  () => props.card.mode === 'terminal' && props.card.availableActions.includes('execute'),
);
const canClose = computed(() => props.card.availableActions.includes('close'));

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
</script>

<style scoped>
.overview-horizontal-terminal {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  background: var(--ac-surface);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
}

.overview-horizontal-terminal__header {
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

.overview-horizontal-terminal__identity {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.overview-horizontal-terminal__title {
  overflow: hidden;
  color: var(--ac-text);
  font-size: 16px;
  font-weight: 700;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-terminal__meta,
.overview-horizontal-terminal__actions {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.overview-horizontal-terminal__meta {
  color: var(--ac-text-muted);
  font-size: 12px;
}

.overview-horizontal-terminal__meta span {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-horizontal-terminal__actions {
  flex: 0 0 auto;
}

.overview-horizontal-terminal__detail {
  min-height: 0;
  flex: 1 1 auto;
}
</style>
