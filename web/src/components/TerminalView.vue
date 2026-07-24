<template>
  <div class="terminal-view">
    <div ref="terminalHost" class="terminal-view__host" aria-label="Terminal 终端" />
    <div v-if="$q.screen.lt.sm" class="terminal-view__mobile-keys" aria-label="终端辅助按键">
      <q-btn dense flat no-caps label="Esc" @click="sendKey('\u001b')" />
      <q-btn dense flat no-caps label="Tab" @click="sendKey('\t')" />
      <q-btn dense flat no-caps label="Ctrl-C" @click="sendKey('\u0003')" />
      <q-btn dense flat icon="keyboard_arrow_up" aria-label="向上" @click="sendKey('\u001b[A')" />
      <q-btn dense flat icon="keyboard_arrow_down" aria-label="向下" @click="sendKey('\u001b[B')" />
      <q-btn dense flat icon="keyboard_arrow_left" aria-label="向左" @click="sendKey('\u001b[D')" />
      <q-btn
        dense
        flat
        icon="keyboard_arrow_right"
        aria-label="向右"
        @click="sendKey('\u001b[C')"
      />
    </div>
    <div v-if="!connected && !ended" class="terminal-view__connection text-caption">
      {{ connectionMessage }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import { useQuasar } from 'quasar';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

import { connectTerminal, type TerminalSocket } from '@/services/terminalSocket';

const props = withDefaults(defineProps<{ sessionId: string; interactive?: boolean }>(), {
  interactive: true,
});
const emit = defineEmits<{
  ready: [];
  exit: [];
  error: [message: string];
}>();

const $q = useQuasar();
const terminalHost = ref<HTMLElement | null>(null);
const connected = ref(false);
const hasConnected = ref(false);
const ended = ref(false);
const connectionMessage = computed(() =>
  hasConnected.value ? '连接已断开，正在重连…' : '正在连接 Terminal…',
);
let terminal: Terminal | null = null;
let fitAddon: FitAddon | null = null;
let connection: TerminalSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
let themeObserver: MutationObserver | null = null;
let outputQueue: Uint8Array[] = [];
let outputQueueBytes = 0;
let writingOutput = false;
const maxOutputQueueBytes = 2 << 20;

onMounted(async () => {
  await nextTick();
  if (!terminalHost.value) return;
  terminal = new Terminal({
    cursorBlink: true,
    allowProposedApi: false,
    scrollback: 5000,
    fontFamily: 'JetBrains Mono, SFMono-Regular, Consolas, Liberation Mono, monospace',
    fontSize: $q.screen.lt.sm ? 13 : 14,
    theme: terminalTheme(),
  });
  fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);
  terminal.open(terminalHost.value);
  resizeObserver = new ResizeObserver(fitTerminal);
  resizeObserver.observe(terminalHost.value);
  themeObserver = new MutationObserver(() => {
    if (terminal) terminal.options.theme = terminalTheme();
  });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] });
  connection = connectTerminal(props.sessionId, {
    onReady() {
      outputQueue = [];
      outputQueueBytes = 0;
      writingOutput = false;
      terminal?.reset();
      fitTerminal();
      terminal?.focus();
      emit('ready');
    },
    onOutput(data) {
      if (outputQueueBytes + data.byteLength > maxOutputQueueBytes) {
        connection?.disconnect();
        emit('error', 'Terminal 输出过快，连接已断开');
        return;
      }
      outputQueue.push(data);
      outputQueueBytes += data.byteLength;
      drainOutputQueue();
    },
    onExit() {
      ended.value = true;
      emit('exit');
    },
    onError(message) {
      ended.value = true;
      if (!props.interactive) terminal?.writeln('\r\nTerminal 已停止，暂无可恢复的历史记录。');
      emit('error', message);
    },
    onConnectionChange(value) {
      connected.value = value;
      if (value) hasConnected.value = true;
    },
  });
  terminal.onData((data) => {
    if (props.interactive) connection?.send(data);
  });
  fitTerminal();
});

onBeforeUnmount(() => {
  connection?.disconnect();
  resizeObserver?.disconnect();
  themeObserver?.disconnect();
  terminal?.dispose();
});

function fitTerminal() {
  if (!terminal || !fitAddon || !terminalHost.value || terminalHost.value.clientWidth === 0) return;
  fitAddon.fit();
  if (props.interactive) connection?.resize(terminal.cols, terminal.rows);
}

function sendKey(data: string) {
  connection?.send(data);
  terminal?.focus();
}

function drainOutputQueue() {
  if (writingOutput || !terminal) return;
  const chunk = outputQueue.shift();
  if (!chunk) return;
  writingOutput = true;
  terminal.write(chunk, () => {
    outputQueueBytes = Math.max(0, outputQueueBytes - chunk.byteLength);
    writingOutput = false;
    connection?.acknowledge(chunk.byteLength);
    drainOutputQueue();
  });
}

function terminalTheme() {
  const style = getComputedStyle(document.documentElement);
  return {
    background: style.getPropertyValue('--ac-surface').trim(),
    foreground: style.getPropertyValue('--ac-text').trim(),
    cursor: style.getPropertyValue('--q-primary').trim(),
    selectionBackground: style.getPropertyValue('--ac-border-strong').trim(),
  };
}
</script>

<style scoped>
.terminal-view {
  position: relative;
  display: flex;
  min-width: 0;
  min-height: 280px;
  flex: 1 1 auto;
  flex-direction: column;
  overflow: hidden;
  background: var(--ac-surface);
}

.terminal-view__host {
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  padding: 8px;
}

.terminal-view__mobile-keys {
  display: flex;
  flex: 0 0 auto;
  overflow-x: auto;
  border-top: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.terminal-view__connection {
  position: absolute;
  top: 8px;
  right: 12px;
  padding: 3px 8px;
  color: var(--ac-text-muted);
  border-radius: 4px;
  background: var(--ac-surface-raised);
}
</style>
