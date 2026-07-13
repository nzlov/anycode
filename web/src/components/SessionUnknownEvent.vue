<template>
  <article class="unknown-event">
    <button type="button" class="unknown-event__header" @click="expanded = !expanded">
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="data_object" size="16px" />
      <span>{{ event.content.rawType || '未知事件' }}</span>
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <pre v-if="expanded">{{ formattedPayload }}</pre>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import type { TranscriptItem, TranscriptUnknownContent } from '@/services/sessionTimeline';
import { timelineTime } from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptUnknownContent };
}>();
const expanded = ref(false);
const formattedPayload = computed(() => JSON.stringify(props.event.content.payload, null, 2));
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
