<template>
  <article class="status-event-shell">
    <div class="status-event" :class="`status-event--${content.level}`">
      <q-icon :name="statusIcon(content)" :color="statusColor(content)" size="16px" />
      <div class="status-event__content">
        <strong>{{ statusLabel(content) }}</strong>
        <span v-if="content.message">{{ content.message }}</span>
      </div>
      <q-btn
        v-if="hasDetails"
        flat
        dense
        round
        size="sm"
        icon="code"
        :aria-label="expanded ? '收起事件详情' : '查看事件详情'"
        @click="expanded = !expanded"
      >
        <q-tooltip>{{ expanded ? '收起详情' : '查看详情' }}</q-tooltip>
      </q-btn>
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </div>
    <StructuredContent v-if="expanded" :content="detailsContent" />
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import StructuredContent from '@/components/StructuredContent.vue';
import type {
  SessionStatusContent,
  SessionStructuredText,
  SessionTimelineItem,
} from '@/services/sessionTimeline';
import {
  statusColor,
  statusIcon,
  statusLabel,
  timelineTime,
} from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: SessionTimelineItem & { content: SessionStatusContent };
}>();
const content = computed(() => props.event.content);
const expanded = ref(false);
const hasDetails = computed(() => Object.keys(content.value.details).length > 0);
const detailsContent = computed<SessionStructuredText>(() => ({
  format: 'json',
  text: JSON.stringify(content.value.details),
}));
</script>

<style scoped>
.status-event {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto auto;
  align-items: start;
  gap: 8px;
  padding: 5px 8px;
  color: var(--ac-text-muted);
  font-size: 13px;
}

.status-event--warning {
  background: color-mix(in srgb, var(--q-warning) 9%, transparent);
}

.status-event--error {
  background: color-mix(in srgb, var(--q-negative) 8%, transparent);
}

.status-event-shell > :deep(.structured-content) {
  margin: 4px 8px 0 32px;
}

.status-event__content {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 4px 8px;
}

.status-event__content strong {
  color: var(--ac-text);
  font-weight: 600;
}

.status-event__content span {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.status-event time {
  font-size: 12px;
}
</style>
