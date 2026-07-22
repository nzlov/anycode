<template>
  <section v-if="event.group" class="event-group">
    <button
      type="button"
      class="event-group__toggle"
      :aria-expanded="groupExpanded"
      @click="groupExpanded = !groupExpanded"
    >
      <q-icon :name="groupExpanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <span>{{ groupLabel }}</span>
      <q-badge outline class="text-muted" :label="String(event.group.count)" />
    </button>
    <div v-if="groupExpanded" class="event-group__members">
      <SessionEventMessage
        v-for="member in groupMembers"
        :key="member.id"
        :event="member"
        :known-user-prompts="knownUserPrompts"
      />
    </div>
  </section>
  <SessionTextMessage
    v-else-if="event.content.__typename === 'TranscriptMessageContent'"
    :event="textEvent"
    :known-user-prompts="knownUserPrompts"
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
  <SessionArtifactEvent
    v-else-if="
      event.content.__typename === 'TranscriptUnknownContent' &&
      event.content.rawType.startsWith('artifact.')
    "
    :event="unknownEvent"
  />
  <SessionUnknownEvent v-else :event="unknownEvent" />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import SessionCommandEvent from '@/components/SessionCommandEvent.vue';
import SessionArtifactEvent from '@/components/SessionArtifactEvent.vue';
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
  }>(),
  { knownUserPrompts: () => [] },
);

const groupExpanded = ref(false);
const groupLabel = computed(() => {
  switch (props.event.group?.kind) {
    case 'lifecycle':
      return '运行状态';
    case 'artifact':
      return '临时文件';
    default:
      return props.event.group?.label ?? '事件';
  }
});
const groupMembers = computed<TranscriptItem[]>(() =>
  (props.event.group?.members ?? []).map((member) => ({
    ...member,
    sourceEventIds: [member.id],
  })),
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

<style scoped>
.event-group__toggle {
  display: flex;
  width: 100%;
  min-height: 34px;
  align-items: center;
  gap: 8px;
  border: 0;
  background: transparent;
  color: var(--ac-text-muted);
  cursor: pointer;
  font: inherit;
  text-align: left;
}

.event-group__toggle:focus-visible {
  outline: 2px solid var(--q-primary);
  outline-offset: 2px;
}

.event-group__toggle span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.event-group__members {
  display: grid;
  gap: 4px;
  padding-left: 18px;
  border-left: 1px solid var(--ac-border);
}
</style>
