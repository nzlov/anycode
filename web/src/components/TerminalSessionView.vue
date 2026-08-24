<template>
  <q-page
    class="terminal-session-page page-shell"
    :style="{ '--terminal-session-viewport-bottom': visualViewportBottom }"
  >
    <PageToolbar :title="session?.title || 'Terminal'" title-icon="terminal">
      <q-badge
        v-if="session"
        outline
        :color="statusColor(session.status)"
        :label="statusLabel(session.status)"
      />
      <q-btn
        v-if="!$q.screen.lt.sm && canStop"
        flat
        round
        dense
        color="negative"
        icon="stop"
        aria-label="停止 Terminal"
        :loading="action === 'stop'"
        @click="stop"
      >
        <q-tooltip>停止 Terminal</q-tooltip>
      </q-btn>
      <q-btn
        v-if="!$q.screen.lt.sm && canStart"
        flat
        round
        dense
        color="positive"
        icon="play_arrow"
        aria-label="启动 Terminal"
        :loading="action === 'start'"
        @click="start"
      >
        <q-tooltip>启动 Terminal</q-tooltip>
      </q-btn>
      <q-btn
        v-if="!$q.screen.lt.sm && canClose"
        flat
        round
        dense
        color="negative"
        icon="close"
        aria-label="关闭 Terminal 卡片"
        :loading="action === 'close'"
        @click="close"
      >
        <q-tooltip>关闭 Terminal 卡片</q-tooltip>
      </q-btn>
    </PageToolbar>

    <q-card flat bordered class="terminal-session-card">
      <TerminalView
        v-if="session && session.status !== 'closed'"
        :key="terminalGeneration"
        :session-id="sessionId"
        :interactive="session.status === 'running'"
        @exit="handleExit"
        @error="terminalError = $event"
      />
      <div v-else-if="loading" class="terminal-session-state flex flex-center">
        <q-spinner color="primary" size="32px" />
      </div>
      <div
        v-else-if="session?.status === 'closed'"
        class="terminal-session-state flex flex-center column q-gutter-sm"
      >
        <q-icon name="terminal" size="42px" color="grey" />
        <div>{{ terminalError || stoppedMessage }}</div>
        <q-btn
          v-if="canStart"
          outline
          color="primary"
          icon="play_arrow"
          label="启动 Terminal"
          @click="start"
        />
      </div>
      <div
        v-if="$q.screen.lt.sm && session && session.status !== 'closed'"
        class="terminal-session-mobile-actions"
        aria-label="Terminal 状态控制"
      >
        <q-btn
          v-if="canStop"
          flat
          no-caps
          color="negative"
          icon="stop"
          label="停止"
          :loading="action === 'stop'"
          @click="stop"
        />
        <q-btn
          v-if="canStart"
          flat
          no-caps
          color="positive"
          icon="play_arrow"
          label="启动"
          :loading="action === 'start'"
          @click="start"
        />
        <q-btn
          v-if="canClose"
          flat
          no-caps
          color="negative"
          icon="close"
          label="关闭卡片"
          :loading="action === 'close'"
          @click="close"
        />
      </div>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import PageToolbar from '@/components/PageToolbar.vue';
import TerminalView from '@/components/TerminalView.vue';
import {
  closeSession,
  executeSession,
  getSession,
  stopSession,
  type SessionDetail,
} from '@/services/sessions';
import {
  sessionStatusColor as statusColor,
  sessionStatusLabel as statusLabel,
} from '@/services/sessionStatusPresentation';

const props = defineProps<{ sessionId: string }>();
const emit = defineEmits<{ 'session-title': [title: string] }>();
const router = useRouter();
const session = ref<SessionDetail | null>(null);
const loading = ref(true);
const action = ref<'start' | 'stop' | 'close' | ''>('');
const terminalError = ref('');
const terminalGeneration = ref(0);
const visualViewportBottom = ref('100dvh');
let viewportResizeFrame = 0;
const canStop = computed(() => session.value?.availableActions.includes('stop') ?? false);
const canStart = computed(() => session.value?.availableActions.includes('execute') ?? false);
const canClose = computed(() => session.value?.availableActions.includes('close') ?? false);
const stoppedMessage = computed(() =>
  session.value?.status === 'closed' ? 'Terminal 卡片已关闭' : 'Terminal 已停止',
);

onMounted(() => {
  void load();
  syncVisualViewportBottom();
  window.visualViewport?.addEventListener('resize', scheduleVisualViewportSync);
  window.visualViewport?.addEventListener('scroll', scheduleVisualViewportSync);
});

onBeforeUnmount(() => {
  window.visualViewport?.removeEventListener('resize', scheduleVisualViewportSync);
  window.visualViewport?.removeEventListener('scroll', scheduleVisualViewportSync);
  if (viewportResizeFrame) cancelAnimationFrame(viewportResizeFrame);
});

function scheduleVisualViewportSync() {
  if (viewportResizeFrame) cancelAnimationFrame(viewportResizeFrame);
  viewportResizeFrame = requestAnimationFrame(syncVisualViewportBottom);
}

function syncVisualViewportBottom() {
  viewportResizeFrame = 0;
  const viewport = window.visualViewport;
  visualViewportBottom.value = viewport
    ? `${Math.round(viewport.offsetTop + viewport.height)}px`
    : '100dvh';
}

async function load() {
  loading.value = true;
  try {
    session.value = await getSession(props.sessionId);
    emit('session-title', session.value.title || 'Terminal');
  } finally {
    loading.value = false;
  }
}

async function start() {
  action.value = 'start';
  terminalError.value = '';
  try {
    await executeSession(props.sessionId);
    await load();
    terminalGeneration.value += 1;
  } finally {
    action.value = '';
  }
}

async function stop() {
  action.value = 'stop';
  try {
    await stopSession(props.sessionId);
    await load();
  } finally {
    action.value = '';
  }
}

async function close() {
  action.value = 'close';
  try {
    await closeSession(props.sessionId);
    await router.push({ name: 'overview' });
  } finally {
    action.value = '';
  }
}

function handleExit() {
  void load();
}
</script>

<style scoped>
.terminal-session-page {
  box-sizing: border-box;
  display: flex;
  height: calc(var(--terminal-session-viewport-bottom, 100dvh) - 50px);
  min-height: 0 !important;
  flex-direction: column;
}

.terminal-session-card {
  display: flex;
  min-height: 0;
  flex: 1 1 auto;
  flex-direction: column;
  overflow: hidden;
}

.terminal-session-card :deep(.terminal-view) {
  min-height: 0;
}

.terminal-session-mobile-actions {
  display: flex;
  min-height: 48px;
  flex: 0 0 auto;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  padding: 2px max(8px, env(safe-area-inset-right)) calc(2px + env(safe-area-inset-bottom))
    max(8px, env(safe-area-inset-left));
  border-top: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.terminal-session-mobile-actions > :deep(.q-btn) {
  min-height: 44px;
}

.terminal-session-state {
  min-height: 320px;
  flex: 1 1 auto;
  color: var(--ac-text-muted);
}
</style>
