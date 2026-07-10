<template>
  <article class="session-event-message" :class="messageClass">
    <SessionFileChangeEvent v-if="isFileChange" :event="event" />
    <SessionToolEvent v-else-if="isTool" :event="event" />
    <SessionUsageEvent v-else-if="event.kind === 'usage'" :event="event" />
    <SessionStatusEvent v-else-if="event.kind === 'status'" :event="event" />

    <template v-else>
      <div class="event-message__content">
        <div class="event-message__main">
          <div
            v-if="event.kind === 'assistant'"
            class="event-message__body event-message__body--markdown"
            v-html="assistantHtml"
          />
          <div v-else-if="isConversation" class="event-message__body">
            {{ event.body || event.title }}
          </div>
          <SessionEventImages
            :event-id="event.id"
            :images="event.images ?? []"
            label="用户输入图片"
          />
        </div>
        <time v-if="event.time" class="event-message__time">{{ event.time }}</time>
      </div>
    </template>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import { renderMarkdown } from '@/services/sessionEventPresentation';
import SessionEventImages from '@/components/SessionEventImages.vue';
import SessionFileChangeEvent from '@/components/SessionFileChangeEvent.vue';
import SessionStatusEvent from '@/components/SessionStatusEvent.vue';
import SessionToolEvent from '@/components/SessionToolEvent.vue';
import SessionUsageEvent from '@/components/SessionUsageEvent.vue';
import type { SessionEvent } from '@/services/sessions';

const props = defineProps<{
  event: SessionEvent;
}>();

const isTool = computed(() => props.event.kind === 'tool');
const isFileChange = computed(() => props.event.kind === 'file_change');
const isConversation = computed(() =>
  ['user', 'assistant', 'question', 'thought'].includes(props.event.kind),
);
const messageClass = computed(() => ({
  'session-event-message--tool': isTool.value,
  'session-event-message--conversation': isConversation.value,
  'session-event-message--assistant': props.event.kind === 'assistant',
  'session-event-message--status': props.event.kind === 'status',
}));
const assistantHtml = computed(() => renderMarkdown(props.event.body || props.event.title));
</script>

<style scoped>
.session-event-message {
  min-width: 0;
}

.session-event-message--conversation {
  padding: 8px 10px;
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.session-event-message--assistant {
  background: color-mix(in srgb, var(--q-positive) 7%, var(--ac-surface));
}

.event-message__content {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 12px;
}

.event-message__body {
  max-width: 100%;
  color: var(--ac-text);
  font-size: 14px;
  line-height: 1.72;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  word-break: break-word;
}

.event-message__main {
  min-width: 0;
}

.event-message__body--markdown :deep(p),
.event-message__body--markdown :deep(ul),
.event-message__body--markdown :deep(pre) {
  margin: 0 0 8px;
}

.event-message__body--markdown {
  white-space: normal;
}

.event-message__body--markdown :deep(p:last-child),
.event-message__body--markdown :deep(ul:last-child),
.event-message__body--markdown :deep(pre:last-child) {
  margin-bottom: 0;
}

.event-message__body--markdown :deep(ul) {
  padding-left: 20px;
}

.event-message__body--markdown :deep(code) {
  padding: 1px 4px;
  border-radius: 4px;
  background: var(--ac-surface-muted);
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 0.92em;
}

.event-message__body--markdown :deep(pre) {
  overflow: auto;
  padding: 8px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
}

.event-message__body--markdown :deep(pre code) {
  padding: 0;
  background: transparent;
}

.event-message__time {
  color: var(--ac-text-muted);
  font-size: 12px;
  line-height: 1.4;
}

@media (max-width: 699px) {
  .event-message__content {
    gap: 8px;
  }
}
</style>
