<template>
  <SessionTextMessage
    v-if="event.content.__typename === 'SessionTextMessageContent'"
    :event="textEvent"
  />
  <SessionReasoningEvent
    v-else-if="event.content.__typename === 'SessionReasoningContent'"
    :event="reasoningEvent"
  />
  <SessionCommandEvent
    v-else-if="event.content.__typename === 'SessionCommandContent'"
    :event="commandEvent"
  />
  <SessionToolEvent
    v-else-if="event.content.__typename === 'SessionToolContent'"
    :event="toolEvent"
  />
  <SessionFileChangeEvent
    v-else-if="event.content.__typename === 'SessionFileChangeContent'"
    :event="fileChangeEvent"
  />
  <SessionStatusEvent
    v-else-if="event.content.__typename === 'SessionStatusContent'"
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
  SessionCommandContent,
  SessionFileChangeContent,
  SessionReasoningContent,
  SessionStatusContent,
  SessionTextMessageContent,
  SessionTimelineItem,
  SessionToolContent,
  SessionUnknownContent,
} from '@/services/sessionTimeline';

const props = defineProps<{ event: SessionTimelineItem }>();

const textEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionTextMessageContent },
);
const reasoningEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionReasoningContent },
);
const commandEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionCommandContent },
);
const toolEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionToolContent },
);
const fileChangeEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionFileChangeContent },
);
const statusEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionStatusContent },
);
const unknownEvent = computed(
  () => props.event as SessionTimelineItem & { content: SessionUnknownContent },
);
</script>
