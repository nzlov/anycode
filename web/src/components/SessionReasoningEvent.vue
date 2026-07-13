<template>
  <article class="reasoning-event">
    <button type="button" class="reasoning-event__header" @click="expanded = !expanded">
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="psychology" size="16px" />
      <span>思考过程</span>
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <MarkdownContent v-if="expanded" class="reasoning-event__body" :text="event.content.text" />
  </article>
</template>

<script setup lang="ts">
import { ref } from 'vue';

import MarkdownContent from '@/components/MarkdownContent.vue';
import type { TranscriptReasoningContent, TranscriptItem } from '@/services/sessionTimeline';
import { timelineTime } from '@/services/sessionTimelinePresentation';

defineProps<{
  event: TranscriptItem & { content: TranscriptReasoningContent };
}>();
const expanded = ref(false);
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
