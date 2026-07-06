<template>
  <article class="session-event-message" :class="messageClass">
    <button
      v-if="isTool"
      type="button"
      class="event-tool__header"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="terminal" size="16px" />
      <span>{{ toolTitle }}</span>
      <time>{{ event.time }}</time>
    </button>
    <pre v-if="isTool && expanded && event.body" class="event-tool__body">{{ event.body }}</pre>

    <template v-else-if="!isTool">
      <div class="event-message__content">
        <div v-if="isConversation" class="event-message__body">
          {{ event.body || event.title }}
        </div>
        <div v-else class="event-status__body">
          {{ event.body || event.title || '已记录事件' }}
        </div>
        <time v-if="event.time" class="event-message__time">{{ event.time }}</time>
      </div>
    </template>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

export interface SessionEventMessageEntry {
  id: string;
  kind: 'thought' | 'tool' | 'assistant' | 'status' | 'question' | 'user';
  title: string;
  body: string;
  createdAt: string;
  time: string;
  rawType?: string;
}

const props = defineProps<{
  event: SessionEventMessageEntry;
}>();

const expanded = ref(false);
const isTool = computed(() => props.event.kind === 'tool');
const isConversation = computed(() =>
  ['user', 'assistant', 'question', 'thought'].includes(props.event.kind),
);
const messageClass = computed(() => ({
  'session-event-message--tool': isTool.value,
  'session-event-message--conversation': isConversation.value,
  'session-event-message--assistant': props.event.kind === 'assistant',
  'session-event-message--status': props.event.kind === 'status',
}));
const toolTitle = computed(() => {
  const firstLine = props.event.body.split('\n').find((line) => line.trim())?.trim() ?? '';
  if (props.event.title === '执行命令' && firstLine) return `Shell ${firstLine}`;
  if (props.event.title === '命令结果') return 'Shell result';
  return `Shell ${props.event.title}`;
});
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

.event-message__body,
.event-status__body {
  max-width: 100%;
  color: var(--ac-text);
  font-size: 14px;
  line-height: 1.72;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  word-break: break-word;
}

.event-status__body {
  color: var(--ac-text-muted);
  font-size: 13px;
}

.event-message__time {
  color: var(--ac-text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.event-tool__header {
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

.event-tool__header span {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.event-tool__header time {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
  font-family: Roboto, Arial, sans-serif;
  font-size: 12px;
  font-weight: 400;
}

.event-tool__header:hover,
.event-tool__header:focus-visible {
  border-color: color-mix(in srgb, var(--q-primary) 45%, var(--ac-border));
  outline: none;
}

.event-tool__body {
  max-height: 320px;
  margin: 4px 0 0;
  overflow: auto;
  padding: 9px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
  color: var(--ac-text);
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre;
}

@media (max-width: 699px) {
  .event-message__content {
    gap: 8px;
  }

  .event-tool__header {
    align-items: flex-start;
  }

  .event-tool__header span {
    white-space: normal;
  }
}
</style>
