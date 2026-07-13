<template>
  <MarkdownContent v-if="content.format === 'markdown'" :text="content.text" />
  <StaticAnsiOutput v-else-if="content.format === 'ansi'" :text="content.text" />
  <pre v-else class="structured-content"><code>{{ displayText }}</code></pre>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import MarkdownContent from '@/components/MarkdownContent.vue';
import StaticAnsiOutput from '@/components/StaticAnsiOutput.vue';
import type { TranscriptStructuredText } from '@/services/sessionTimeline';

const props = defineProps<{ content: TranscriptStructuredText }>();

const displayText = computed(() => {
  if (props.content.format !== 'json') return props.content.text;
  try {
    return JSON.stringify(JSON.parse(props.content.text), null, 2);
  } catch {
    return props.content.text;
  }
});
</script>

<style scoped>
.structured-content {
  margin: 4px 0 0;
  max-width: 100%;
  overflow: auto;
  padding: 9px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
  color: var(--ac-text);
  cursor: text;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  user-select: text;
  white-space: pre;
}
</style>
