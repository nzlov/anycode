<template>
  <div class="tool-event">
    <button
      type="button"
      class="tool-event__header"
      :class="{ 'session-event-header--sticky': expanded }"
      :aria-expanded="expanded"
      @click="toggleExpanded"
    >
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon name="build" size="16px" />
      <span>{{ displayTitle }}</span>
      <q-spinner
        v-if="loading || event.phase === 'started' || event.phase === 'progress'"
        size="14px"
      />
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
        <q-banner v-if="error" dense class="text-negative">{{ error }}</q-banner>
        <template v-if="isQuestionEvent">
          <q-banner v-if="questionError" dense class="text-negative">
            {{ questionError }}
          </q-banner>
          <QuestionsPanel
            v-else
            :requests="questionRequest ? [questionRequest] : []"
            :loading="questionLoading || loading"
          />
        </template>
        <section v-else-if="content.input.text" class="tool-event__section">
          <div class="tool-event__label">输入</div>
          <StructuredContent :content="content.input" />
        </section>
        <section v-if="!isQuestionEvent && content.output.text" class="tool-event__section">
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

import QuestionsPanel from '@/components/QuestionsPanel.vue';
import SessionEventImages from '@/components/SessionEventImages.vue';
import StructuredContent from '@/components/StructuredContent.vue';
import { useDeferredTranscriptEvent } from '@/composables/useDeferredTranscriptEvent';
import { getQuestionRequest, type QuestionRequest } from '@/services/sessions';
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
const {
  event: resolvedEvent,
  loading,
  error,
  load,
} = useDeferredTranscriptEvent(() => props.event);
const content = computed(() => resolvedEvent.value.content as TranscriptToolContent);
const displayTitle = computed(() => toolLabel(content.value));
const isQuestionEvent = computed(
  () =>
    content.value.qualifiedName === 'questions' ||
    content.value.qualifiedName.endsWith('.questions'),
);
const questionRequest = ref<QuestionRequest | null>(null);
const questionLoading = ref(false);
const questionError = ref('');

async function toggleExpanded() {
  expanded.value = !expanded.value;
  if (!expanded.value) return;
  await load();
  if (isQuestionEvent.value) await loadQuestionRequest();
}

async function loadQuestionRequest() {
  const requestId = questionRequestId(content.value.output.text);
  if (!requestId) {
    questionError.value =
      resolvedEvent.value.phase === 'started' || resolvedEvent.value.phase === 'progress'
        ? '问题正在等待回答，完成后将在此显示。'
        : '无法识别问题请求。';
    return;
  }
  if (questionRequest.value?.id === requestId || questionLoading.value) return;
  questionLoading.value = true;
  questionError.value = '';
  try {
    questionRequest.value = await getQuestionRequest(requestId);
  } catch (err) {
    questionError.value = err instanceof Error ? err.message : '加载问题和回答失败';
  } finally {
    questionLoading.value = false;
  }
}

function questionRequestId(output: string): string {
  // GLUE: Timeline tool output carries only the question request ID; remove when it is a typed field.
  try {
    const value = JSON.parse(output) as { requestId?: unknown };
    return typeof value.requestId === 'string' ? value.requestId : '';
  } catch {
    return '';
  }
}
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
