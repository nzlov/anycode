<template>
  <pre class="static-ansi-output"><span
    v-for="(segment, index) in segments"
    :key="index"
    :style="segmentStyle(segment)"
  >{{ segment.content }}</span></pre>
</template>

<script setup lang="ts">
import Anser from 'anser';
import { computed } from 'vue';

import { stripUnsupportedAnsiControls } from '@/services/sessionTimelinePresentation';

interface AnsiSegment {
  content: string;
  fg?: string | null;
  bg?: string | null;
  decoration?: string | null;
  decorations?: string[];
}

const props = defineProps<{ text: string }>();

const segments = computed(
  () => Anser.ansiToJson(stripUnsupportedAnsiControls(props.text || '')) as AnsiSegment[],
);

const themedColors: Record<string, string> = {
  '0,0,0': 'var(--ac-ansi-black)',
  '187,0,0': 'var(--ac-ansi-red)',
  '0,187,0': 'var(--ac-ansi-green)',
  '187,187,0': 'var(--ac-ansi-yellow)',
  '0,0,187': 'var(--ac-ansi-blue)',
  '187,0,187': 'var(--ac-ansi-magenta)',
  '0,187,187': 'var(--ac-ansi-cyan)',
  '255,255,255': 'var(--ac-ansi-white)',
  '85,85,85': 'var(--ac-ansi-bright-black)',
  '255,85,85': 'var(--ac-ansi-bright-red)',
  '0,255,0': 'var(--ac-ansi-bright-green)',
  '255,255,85': 'var(--ac-ansi-bright-yellow)',
  '85,85,255': 'var(--ac-ansi-bright-blue)',
  '255,85,255': 'var(--ac-ansi-bright-magenta)',
  '85,255,255': 'var(--ac-ansi-bright-cyan)',
};

function segmentStyle(segment: AnsiSegment) {
  const style: Record<string, string> = {};
  if (segment.fg) style.color = ansiColor(segment.fg);
  if (segment.bg) style.backgroundColor = ansiColor(segment.bg);
  const decorations = new Set(segment.decorations ?? []);
  if (segment.decoration) decorations.add(segment.decoration);
  if (decorations.has('bold')) style.fontWeight = '700';
  if (decorations.has('italic')) style.fontStyle = 'italic';
  const textDecorations = [];
  if (decorations.has('underline')) textDecorations.push('underline');
  if (decorations.has('strikethrough')) textDecorations.push('line-through');
  if (textDecorations.length > 0) style.textDecoration = textDecorations.join(' ');
  if (decorations.has('dim')) style.opacity = '0.65';
  if (decorations.has('hidden')) style.visibility = 'hidden';
  return style;
}

function ansiColor(value: string) {
  const normalized = value.replaceAll(' ', '');
  return themedColors[normalized] ?? `rgb(${normalized})`;
}
</script>

<style scoped>
.static-ansi-output {
  margin: 4px 0 0;
  max-width: 100%;
  overflow: auto;
  padding: 9px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-terminal-bg, var(--ac-surface));
  color: var(--ac-terminal-fg, var(--ac-text));
  cursor: text;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  tab-size: 2;
  user-select: text;
  white-space: pre;
}
</style>
