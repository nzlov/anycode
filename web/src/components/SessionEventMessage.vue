<template>
  <SessionTextMessage
    v-if="event.content.__typename === 'TranscriptMessageContent'"
    :event="textEvent"
    :known-user-prompts="knownUserPrompts"
    :workflow-prompt="workflowPrompt"
  />
  <SessionReasoningEvent
    v-else-if="event.content.__typename === 'TranscriptReasoningContent'"
    :event="reasoningEvent"
  />
  <SessionCommandEvent
    v-else-if="event.content.__typename === 'TranscriptCommandContent'"
    :event="commandEvent"
  />
  <SessionToolEvent
    v-else-if="event.content.__typename === 'TranscriptToolContent'"
    :event="toolEvent"
  />
  <SessionFileChangeEvent
    v-else-if="event.content.__typename === 'TranscriptFileChangeContent'"
    :event="fileChangeEvent"
  />
  <SessionStatusEvent
    v-else-if="event.content.__typename === 'TranscriptStatusContent'"
    :event="statusEvent"
  />
  <SessionUnknownEvent v-else :event="unknownEvent" />
</template>

<script setup lang="ts">
import { computed } from 'vue';

import SessionCommandEvent from '@/components/SessionCommandEvent.vue';
import SessionFileChangeEvent from '@/components/SessionFileChangeEvent.vue';
import SessionReasoningEvent from '@/components/SessionReasoningEvent.vue';
import SessionStatusEvent from '@/components/SessionStatusEvent.vue';
import SessionTextMessage from '@/components/SessionTextMessage.vue';
import SessionToolEvent from '@/components/SessionToolEvent.vue';
import SessionUnknownEvent from '@/components/SessionUnknownEvent.vue';
import type {
  TranscriptCommandContent,
  TranscriptFileChangeContent,
  TranscriptReasoningContent,
  TranscriptStatusContent,
  TranscriptMessageContent,
  TranscriptItem,
  TranscriptToolContent,
  TranscriptUnknownContent,
} from '@/services/sessionTimeline';

const props = withDefaults(
  defineProps<{
    event: TranscriptItem;
    knownUserPrompts?: readonly string[];
    workflowPrompt?: boolean;
  }>(),
  { knownUserPrompts: () => [], workflowPrompt: false },
);

const textEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptMessageContent },
);
const reasoningEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptReasoningContent },
);
const commandEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptCommandContent },
);
const toolEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptToolContent },
);
const fileChangeEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptFileChangeContent },
);
const statusEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptStatusContent },
);
const unknownEvent = computed(
  () => props.event as TranscriptItem & { content: TranscriptUnknownContent },
);
</script>
