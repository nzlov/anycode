<template>
  <article class="unknown-event">
    <button type="button" class="unknown-event__header" @click="toggleExpanded">
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="data_object" size="16px" />
      <span>{{ event.content.rawType || '未知事件' }}</span>
      <q-spinner v-if="loading" size="14px" />
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <q-banner v-if="expanded && error" dense class="text-negative">{{ error }}</q-banner>
    <pre v-if="expanded && !loading">{{ formattedPayload }}</pre>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import { useDeferredTranscriptEvent } from '@/composables/useDeferredTranscriptEvent';
import type { TranscriptItem, TranscriptUnknownContent } from '@/services/sessionTimeline';
import { timelineTime } from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptUnknownContent };
}>();
const expanded = ref(false);
const {
  event: resolvedEvent,
  loading,
  error,
  load,
} = useDeferredTranscriptEvent(() => props.event);
const content = computed(() => resolvedEvent.value.content as TranscriptUnknownContent);
const formattedPayload = computed(() => JSON.stringify(content.value.payload, null, 2));

function toggleExpanded() {
  expanded.value = !expanded.value;
  if (expanded.value) void load();
}
</script>

<style scoped>
.unknown-event__header {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border: 0;
  background: transparent;
  color: var(--ac-text-muted);
  cursor: pointer;
  text-align: left;
}

.unknown-event__header span {
  flex: 1 1 auto;
}

.unknown-event pre {
  margin: 4px 8px 8px 32px;
  overflow: auto;
  padding: 8px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  color: var(--ac-text);
  cursor: text;
  font-size: 12px;
  user-select: text;
  white-space: pre;
}
</style>
