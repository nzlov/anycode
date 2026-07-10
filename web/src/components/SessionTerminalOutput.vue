<template>
  <div ref="terminalElement" class="session-terminal-output" />
</template>

<script setup lang="ts">
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { prepareTerminalOutput } from '@/services/sessionEventPresentation';

const props = defineProps<{
  body: string;
}>();

const terminalElement = ref<HTMLElement | null>(null);
let terminal: Terminal | null = null;

function terminalRows(body: string) {
  const lines = prepareTerminalOutput(body).split(/\r\n|\r|\n/).length;
  return Math.min(16, Math.max(4, lines + 1));
}

function renderTerminal() {
  if (!terminal) return;
  terminal.resize(120, terminalRows(props.body));
  terminal.reset();
  terminal.write(prepareTerminalOutput(props.body));
}

onMounted(() => {
  if (!terminalElement.value) return;
  terminal = new Terminal({
    cols: 120,
    rows: terminalRows(props.body),
    convertEol: true,
    cursorBlink: false,
    disableStdin: true,
    fontFamily: "'Fira Code', 'JetBrains Mono', monospace",
    fontSize: 12,
    lineHeight: 1.6,
    scrollback: 5000,
    theme: {
      background: '#ffffff',
      foreground: '#263238',
      cursor: '#263238',
      black: '#263238',
      red: '#c62828',
      green: '#2e7d32',
      yellow: '#f9a825',
      blue: '#1565c0',
      magenta: '#7b1fa2',
      cyan: '#00838f',
      white: '#eceff1',
      brightBlack: '#607d8b',
      brightRed: '#ef5350',
      brightGreen: '#66bb6a',
      brightYellow: '#ffca28',
      brightBlue: '#42a5f5',
      brightMagenta: '#ab47bc',
      brightCyan: '#26c6da',
      brightWhite: '#ffffff',
    },
  });
  terminal.open(terminalElement.value);
  renderTerminal();
});

watch(() => props.body, renderTerminal, { flush: 'post' });

onBeforeUnmount(() => {
  terminal?.dispose();
  terminal = null;
});
</script>

<style scoped>
.session-terminal-output {
  margin: 4px 0 0;
  overflow-x: auto;
  overflow-y: hidden;
  padding: 9px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.session-terminal-output :deep(.xterm) {
  padding: 0;
}

.session-terminal-output :deep(.xterm-viewport) {
  background: transparent !important;
}
</style>
