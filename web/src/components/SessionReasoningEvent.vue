<template>
  <article class="reasoning-event">
    <button type="button" class="reasoning-event__header" @click="toggleExpanded">
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="psychology" size="16px" />
      <span>思考过程</span>
      <q-spinner v-if="loading" size="14px" />
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <q-banner v-if="expanded && error" dense class="text-negative">{{ error }}</q-banner>
    <MarkdownContent v-if="expanded" class="reasoning-event__body" :text="content.text" />
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import MarkdownContent from '@/components/MarkdownContent.vue';
import { useDeferredTranscriptEvent } from '@/composables/useDeferredTranscriptEvent';
import type { TranscriptReasoningContent, TranscriptItem } from '@/services/sessionTimeline';
import { timelineTime } from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptReasoningContent };
}>();
const expanded = ref(false);
const {
  event: resolvedEvent,
  loading,
  error,
  load,
} = useDeferredTranscriptEvent(() => props.event);
const content = computed(() => resolvedEvent.value.content as TranscriptReasoningContent);

function toggleExpanded() {
  expanded.value = !expanded.value;
  if (expanded.value) void load();
}
</script>

<style scoped>
.reasoning-event__header {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border: 0;
  background: transparent;
  color: var(--ac-text-muted);
  cursor: pointer;
  font-size: 13px;
  text-align: left;
}

.reasoning-event__header span {
  flex: 1 1 auto;
}

.reasoning-event__header time {
  font-size: 12px;
}

.reasoning-event__header:hover,
.reasoning-event__header:focus-visible {
  color: var(--ac-text);
  outline: none;
}

.reasoning-event__body {
  margin: 4px 8px 8px 32px;
  padding-left: 10px;
  border-left: 2px solid var(--ac-border);
}
</style>
