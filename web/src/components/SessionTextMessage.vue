<template>
  <article class="text-message" :class="`text-message--${content.role}`">
    <div class="text-message__main">
      <MarkdownContent
        v-if="presentation.text && content.format === 'markdown'"
        :text="presentation.text"
      />
      <div v-else-if="presentation.text" class="text-message__plain">
        {{ presentation.text }}
      </div>
      <button
        v-if="presentation.foldedText"
        type="button"
        class="text-message__fold-toggle"
        :aria-expanded="expanded"
        @click="expanded = !expanded"
      >
        <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
        <span>{{ presentation.foldedLabel }}</span>
      </button>
      <div v-if="expanded && presentation.foldedText" class="text-message__folded">
        {{ presentation.foldedText }}
      </div>
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
import { computed, ref } from 'vue';

import MarkdownContent from '@/components/MarkdownContent.vue';
import SessionEventImages from '@/components/SessionEventImages.vue';
import type { TranscriptMessageContent, TranscriptItem } from '@/services/sessionTimeline';
import { sessionTextPresentation, timelineTime } from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptMessageContent };
  knownUserPrompts: readonly string[];
  workflowPrompt: boolean;
}>();
const content = computed(() => props.event.content);
const presentation = computed(() =>
  sessionTextPresentation(
    content.value.role,
    content.value.text,
    props.knownUserPrompts,
    props.workflowPrompt,
  ),
);
const expanded = ref(false);
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

.text-message__fold-toggle {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 6px;
  padding: 4px 0;
  border: 0;
  background: transparent;
  color: var(--ac-text-muted);
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.text-message__fold-toggle:hover,
.text-message__fold-toggle:focus-visible {
  color: var(--ac-text);
}

.text-message__fold-toggle:focus-visible {
  outline: 2px solid var(--q-primary);
  outline-offset: 2px;
}

.text-message__folded {
  max-height: 320px;
  overflow: auto;
  margin-top: 4px;
  padding: 8px 10px;
  border-left: 2px solid var(--ac-border);
  background: var(--ac-surface-muted);
  color: var(--ac-text-muted);
  font-size: 12px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  user-select: text;
  white-space: pre-wrap;
}

.text-message time {
  color: var(--ac-text-muted);
  font-size: 12px;
}
</style>
