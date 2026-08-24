<template>
  <div class="terminal-view">
    <div
      ref="terminalHost"
      class="terminal-view__host terminal-view__host--native-selection"
      aria-label="Terminal 终端"
    />
    <div v-if="$q.screen.lt.sm" class="terminal-view__mobile-keys" aria-label="终端辅助按键">
      <q-btn dense flat no-caps label="Esc" :disable="!interactive" @click="sendKey('\u001b')" />
      <q-btn dense flat no-caps label="Tab" :disable="!interactive" @click="sendKey('\t')" />
      <q-btn
        dense
        flat
        no-caps
        label="Ctrl"
        :disable="!interactive"
        :class="{ 'terminal-view__modifier--pressed': isModifierPressed('ctrl') }"
        :aria-pressed="isModifierPressed('ctrl')"
        @click="toggleModifier('ctrl')"
      />
      <q-btn
        dense
        flat
        no-caps
        label="Alt"
        :disable="!interactive"
        :class="{ 'terminal-view__modifier--pressed': isModifierPressed('alt') }"
        :aria-pressed="isModifierPressed('alt')"
        @click="toggleModifier('alt')"
      />
      <q-btn
        dense
        flat
        icon="keyboard_arrow_up"
        aria-label="向上"
        :disable="!interactive"
        @click="sendKey('\u001b[A')"
      />
      <q-btn
        dense
        flat
        icon="keyboard_arrow_down"
        aria-label="向下"
        :disable="!interactive"
        @click="sendKey('\u001b[B')"
      />
      <q-btn
        dense
        flat
        icon="keyboard_arrow_left"
        aria-label="向左"
        :disable="!interactive"
        @click="sendKey('\u001b[D')"
      />
      <q-btn
        dense
        flat
        icon="keyboard_arrow_right"
        aria-label="向右"
        :disable="!interactive"
        @click="sendKey('\u001b[C')"
      />
    </div>
    <div v-if="!connected && !ended" class="terminal-view__connection text-caption">
      {{ connectionMessage }}
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

import { connectTerminal, type TerminalSocket } from '@/services/terminalSocket';

const props = withDefaults(
  defineProps<{ sessionId: string; interactive?: boolean; resizePaused?: boolean }>(),
  {
    interactive: true,
    resizePaused: false,
  },
);
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
type TerminalModifier = 'ctrl' | 'alt';
const pressedModifiers = ref<Set<TerminalModifier>>(new Set());
const connectionMessage = computed(() =>
  hasConnected.value ? '连接已断开，正在重连…' : '正在连接 Terminal…',
);
let terminal: Terminal | null = null;
let fitAddon: FitAddon | null = null;
let connection: TerminalSocket | null = null;
let resizeObserver: ResizeObserver | null = null;
let resizePending = false;
let themeObserver: MutationObserver | null = null;
let touchScrollY: number | null = null;
let outputQueue: Uint8Array[] = [];
let outputQueueBytes = 0;
let writingOutput = false;
const maxOutputQueueBytes = 2 << 20;

watch(
  () => props.resizePaused,
  (paused) => {
    if (!paused && resizePending) void nextTick(fitTerminal);
  },
);

onMounted(async () => {
  await nextTick();
  if (!terminalHost.value) return;
  terminal = new Terminal({
    cursorBlink: true,
    allowProposedApi: false,
    scrollback: 5000,
    screenReaderMode: true,
    fontFamily: 'JetBrains Mono, SFMono-Regular, Consolas, Liberation Mono, monospace',
    fontSize: $q.screen.lt.sm ? 13 : 14,
    theme: terminalTheme(),
  });
  fitAddon = new FitAddon();
  terminal.loadAddon(fitAddon);
  terminal.open(terminalHost.value);
  // GLUE: xterm 6 intercepts mouse selection and renders text to canvas; remove these listeners when it supports native selection.
  terminal.element?.addEventListener('mousedown', handleNativeSelectionMouseDown, true);
  terminal.element?.addEventListener('contextmenu', handleNativeSelectionContextMenu, true);
  terminal.element?.addEventListener('click', handleNativeSelectionClick);
  terminalHost.value.addEventListener('touchstart', handleTouchStart, { passive: true });
  terminalHost.value.addEventListener('touchmove', handleTouchMove, { passive: false });
  terminalHost.value.addEventListener('touchend', handleTouchEnd, { passive: true });
  terminalHost.value.addEventListener('touchcancel', handleTouchEnd, { passive: true });
  resizeObserver = new ResizeObserver(fitTerminal);
  resizeObserver.observe(terminalHost.value);
  themeObserver = new MutationObserver(() => {
    if (terminal) terminal.options.theme = terminalTheme();
  });
  // GLUE: xterm cannot consume CSS variables directly; keep its imperative palette at the CSS theme boundary.
  themeObserver.observe(document.body, { attributes: true, attributeFilter: ['class'] });
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['style'] });
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
    sendInput(data);
  });
  fitTerminal();
});

onBeforeUnmount(() => {
  terminal?.element?.removeEventListener('mousedown', handleNativeSelectionMouseDown, true);
  terminal?.element?.removeEventListener('contextmenu', handleNativeSelectionContextMenu, true);
  terminal?.element?.removeEventListener('click', handleNativeSelectionClick);
  terminalHost.value?.removeEventListener('touchstart', handleTouchStart);
  terminalHost.value?.removeEventListener('touchmove', handleTouchMove);
  terminalHost.value?.removeEventListener('touchend', handleTouchEnd);
  terminalHost.value?.removeEventListener('touchcancel', handleTouchEnd);
  connection?.disconnect();
  resizeObserver?.disconnect();
  themeObserver?.disconnect();
  terminal?.dispose();
});

function fitTerminal() {
  if (props.resizePaused) {
    resizePending = true;
    return;
  }
  if (!terminal || !fitAddon || !terminalHost.value || terminalHost.value.clientWidth === 0) return;
  resizePending = false;
  fitAddon.fit();
  if (props.interactive) connection?.resize(terminal.cols, terminal.rows);
}

function sendKey(data: string) {
  sendInput(data);
  terminal?.focus();
}

function sendInput(data: string) {
  if (!props.interactive) return;
  let input = data;
  if (isModifierPressed('ctrl')) input = controlSequence(input);
  if (isModifierPressed('alt')) input = `\u001b${input}`;
  connection?.send(input);
  pressedModifiers.value = new Set();
}

function toggleModifier(modifier: TerminalModifier) {
  const next = new Set(pressedModifiers.value);
  if (next.has(modifier)) next.delete(modifier);
  else next.add(modifier);
  pressedModifiers.value = next;
  terminal?.focus();
}

function isModifierPressed(modifier: TerminalModifier) {
  return pressedModifiers.value.has(modifier);
}

function controlSequence(data: string) {
  if (data.length !== 1) return data;
  const upper = data.toUpperCase();
  if (upper >= 'A' && upper <= 'Z') return String.fromCharCode(upper.charCodeAt(0) - 64);
  const controls: Record<string, string> = {
    ' ': '\u0000',
    '@': '\u0000',
    '[': '\u001b',
    '\\': '\u001c',
    ']': '\u001d',
    '^': '\u001e',
    _: '\u001f',
    '?': '\u007f',
  };
  return controls[data] ?? data;
}

function handleTouchStart(event: TouchEvent) {
  touchScrollY = event.touches.length === 1 ? (event.touches[0]?.clientY ?? null) : null;
}

function handleTouchMove(event: TouchEvent) {
  if (!terminal || touchScrollY === null || event.touches.length !== 1) return;
  if (hasNativeTerminalSelection()) {
    touchScrollY = null;
    return;
  }
  const currentY = event.touches[0]?.clientY;
  if (currentY === undefined) return;
  const lineHeight = Math.max(1, (terminalHost.value?.clientHeight ?? 0) / terminal.rows);
  const lines = Math.trunc((touchScrollY - currentY) / lineHeight);
  if (lines === 0) return;
  terminal.scrollLines(lines);
  touchScrollY -= lines * lineHeight;
  event.preventDefault();
}

function hasNativeTerminalSelection() {
  const selection = document.getSelection();
  return Boolean(
    selection &&
    !selection.isCollapsed &&
    selection.anchorNode &&
    terminal?.element?.contains(selection.anchorNode),
  );
}

function handleNativeSelectionMouseDown(event: MouseEvent) {
  if (event.button === 0) event.stopImmediatePropagation();
}

function handleNativeSelectionContextMenu(event: MouseEvent) {
  if (hasNativeTerminalSelection()) event.stopImmediatePropagation();
}

function handleNativeSelectionClick() {
  if (!hasNativeTerminalSelection()) terminal?.focus();
}

function handleTouchEnd() {
  touchScrollY = null;
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
  const style = getComputedStyle(terminalHost.value ?? document.body);
  const color = (name: string) => style.getPropertyValue(name).trim();
  return {
    background: color('--ac-terminal-bg'),
    foreground: color('--ac-terminal-fg'),
    cursor: color('--ac-primary'),
    cursorAccent: color('--ac-terminal-bg'),
    selectionBackground: color('--ac-surface-selected'),
    selectionForeground: color('--ac-text'),
    black: color('--ac-ansi-black'),
    red: color('--ac-ansi-red'),
    green: color('--ac-ansi-green'),
    yellow: color('--ac-ansi-yellow'),
    blue: color('--ac-ansi-blue'),
    magenta: color('--ac-ansi-magenta'),
    cyan: color('--ac-ansi-cyan'),
    white: color('--ac-ansi-white'),
    brightBlack: color('--ac-ansi-bright-black'),
    brightRed: color('--ac-ansi-bright-red'),
    brightGreen: color('--ac-ansi-bright-green'),
    brightYellow: color('--ac-ansi-bright-yellow'),
    brightBlue: color('--ac-ansi-bright-blue'),
    brightMagenta: color('--ac-ansi-bright-magenta'),
    brightCyan: color('--ac-ansi-bright-cyan'),
    brightWhite: color('--ac-ansi-bright-white'),
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
  background: var(--ac-terminal-bg);
}

.terminal-view__host {
  min-width: 0;
  min-height: 0;
  flex: 1 1 auto;
  padding: 8px;
  touch-action: none;
}

.terminal-view__host--native-selection {
  touch-action: auto;
}

.terminal-view__host--native-selection :deep(.xterm) {
  user-select: text;
  -webkit-user-select: text;
}

.terminal-view__host--native-selection :deep(.xterm-accessibility:not(.debug)) {
  pointer-events: auto;
}

.terminal-view__mobile-keys {
  display: flex;
  flex: 0 0 auto;
  overflow-x: auto;
  border-top: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
}

.terminal-view__mobile-keys > :deep(.q-btn) {
  min-width: 44px;
  min-height: 44px;
  flex: 0 0 auto;
}

.terminal-view__modifier--pressed {
  color: var(--q-primary);
  background: color-mix(in srgb, var(--q-primary) 16%, transparent);
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
