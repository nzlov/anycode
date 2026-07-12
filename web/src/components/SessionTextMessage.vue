<template>
  <article class="text-message" :class="`text-message--${content.role}`">
    <div class="text-message__main">
      <MarkdownContent v-if="content.format === 'markdown'" :text="content.text" />
      <div v-else class="text-message__plain">{{ content.text }}</div>
      <SessionEventImages
        :event-id="event.id"
        :images="content.images"
        :label="content.role === 'user' ? '用户输入图片' : '模型输出图片'"
      />
    </div>
    <time>{{ timelineTime(event.occurredAt) }}</time>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import MarkdownContent from '@/components/MarkdownContent.vue';
import SessionEventImages from '@/components/SessionEventImages.vue';
import type { SessionTextMessageContent, SessionTimelineItem } from '@/services/sessionTimeline';
import { timelineTime } from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: SessionTimelineItem & { content: SessionTextMessageContent };
}>();
const content = computed(() => props.event.content);
</script>

<style scoped>
.text-message {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 12px;
  min-width: 0;
  padding: 8px 10px;
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.text-message--assistant {
  background: color-mix(in srgb, var(--q-positive) 7%, var(--ac-surface));
}

.text-message__main {
  min-width: 0;
}

.text-message__plain {
  color: var(--ac-text);
  font-size: 14px;
  line-height: 1.72;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.text-message time {
  color: var(--ac-text-muted);
  font-size: 12px;
}
</style>
