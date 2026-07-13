<template>
  <div class="tool-event">
    <button
      type="button"
      class="tool-event__header"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="build" size="16px" />
      <span>{{ displayTitle }}</span>
      <q-spinner v-if="event.phase === 'started' || event.phase === 'progress'" size="14px" />
      <q-icon
        v-else
        :name="timelinePhaseIcon(event.phase)"
        :color="timelinePhaseColor(event.phase)"
        size="16px"
      >
        <q-tooltip>{{ timelinePhaseLabel(event.phase) }}</q-tooltip>
      </q-icon>
      <time>{{ timelineTime(event.occurredAt) }}</time>
    </button>
    <template v-if="expanded">
      <div class="tool-event__content">
        <section v-if="content.input.text" class="tool-event__section">
          <div class="tool-event__label">输入</div>
          <StructuredContent :content="content.input" />
        </section>
        <section v-if="content.output.text" class="tool-event__section">
          <div class="tool-event__label">输出</div>
          <StructuredContent :content="content.output" />
        </section>
        <SessionEventImages :event-id="event.id" :images="content.images" label="工具输出图片" />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import SessionEventImages from '@/components/SessionEventImages.vue';
import StructuredContent from '@/components/StructuredContent.vue';
import type { TranscriptItem, TranscriptToolContent } from '@/services/sessionTimeline';
import {
  timelinePhaseColor,
  timelinePhaseIcon,
  timelinePhaseLabel,
  timelineTime,
  toolLabel,
} from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptToolContent };
}>();
const expanded = ref(false);
const content = computed(() => props.event.content);
const displayTitle = computed(() => toolLabel(content.value));
</script>

<style scoped>
.tool-event__content {
  display: grid;
  gap: 8px;
  margin-top: 6px;
}

.tool-event__section {
  min-width: 0;
}

.tool-event__label {
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.tool-event__header {
  display: flex;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  color: var(--ac-text);
  cursor: pointer;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  text-align: left;
}

.tool-event__header span {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
  white-space: normal;
  word-break: break-word;
}

.tool-event__header time {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
  font-family: Roboto, Arial, sans-serif;
  font-size: 12px;
  font-weight: 400;
}

.tool-event__header:hover,
.tool-event__header:focus-visible {
  border-color: color-mix(in srgb, var(--q-primary) 45%, var(--ac-border));
  outline: none;
}

@media (max-width: 699px) {
  .tool-event__header {
    align-items: flex-start;
  }
}
</style>
