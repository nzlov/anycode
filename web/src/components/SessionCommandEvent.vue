<template>
  <article class="command-event">
    <button
      type="button"
      class="command-event__header"
      :aria-expanded="canExpand ? expanded : undefined"
      :disabled="!canExpand"
      @click="canExpand && (expanded = !expanded)"
    >
      <q-icon v-if="canExpand" :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <span v-else class="command-event__toggle-placeholder" aria-hidden="true" />
      <q-icon name="terminal" size="16px" />
      <span class="command-event__title">{{ firstCommand?.command || 'Shell' }}</span>
      <q-badge
        v-if="additionalCommandCount"
        outline
        color="primary"
        :label="`+${additionalCommandCount} 条`"
      />
      <q-badge
        v-if="content.exitCode !== null"
        outline
        :color="content.exitCode === 0 ? 'positive' : 'negative'"
        :label="`exit ${content.exitCode}`"
      />
      <small v-if="duration">{{ duration }}</small>
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
    <div v-if="expanded && canExpand" class="command-event__body">
      <div v-if="firstCommand?.workdir" class="command-event__workdir">
        {{ firstCommand.workdir }}
      </div>
      <section v-for="(command, index) in additionalCommands" :key="index">
        <div class="command-event__label">命令 {{ index + 2 }}</div>
        <pre class="command-event__command"><code>{{ command.command }}</code></pre>
        <div v-if="command.workdir" class="command-event__workdir">{{ command.workdir }}</div>
      </section>
      <section v-if="content.output">
        <div class="command-event__label">输出</div>
        <StaticAnsiOutput :text="content.output" />
      </section>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import StaticAnsiOutput from '@/components/StaticAnsiOutput.vue';
import type { TranscriptCommandContent, TranscriptItem } from '@/services/sessionTimeline';
import {
  formatDuration,
  timelinePhaseColor,
  timelinePhaseIcon,
  timelinePhaseLabel,
  timelineTime,
} from '@/services/sessionTimelinePresentation';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptCommandContent };
}>();

const expanded = ref(false);
const content = computed(() => props.event.content);
const firstCommand = computed(() => content.value.commands[0]);
const additionalCommands = computed(() => content.value.commands.slice(1));
const additionalCommandCount = computed(() => additionalCommands.value.length);
const canExpand = computed(
  () => Boolean(firstCommand.value?.workdir || additionalCommands.value.length || content.value.output),
);
const duration = computed(() => formatDuration(content.value.durationMs));
</script>

<style scoped>
.command-event {
  min-width: 0;
}

.command-event__header {
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

.command-event__title {
  flex: 1 1 auto;
  min-width: 0;
  overflow-wrap: anywhere;
}

.command-event__header time,
.command-event__header small {
  flex: 0 0 auto;
  color: var(--ac-text-muted);
  font-family: Roboto, Arial, sans-serif;
  font-size: 12px;
  font-weight: 400;
}

.command-event__header:not(:disabled):hover,
.command-event__header:not(:disabled):focus-visible {
  border-color: color-mix(in srgb, var(--q-primary) 45%, var(--ac-border));
  outline: none;
}

.command-event__header:disabled {
  cursor: default;
  opacity: 1;
}

.command-event__toggle-placeholder {
  width: 18px;
  flex: 0 0 18px;
}

.command-event__body {
  display: grid;
  gap: 8px;
  margin-top: 6px;
}

.command-event__label {
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.command-event__command {
  margin: 4px 0 0;
  overflow: auto;
  padding: 9px 10px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
  color: var(--ac-text);
  cursor: text;
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 12px;
  line-height: 1.6;
  user-select: text;
  white-space: pre;
}

.command-event__workdir {
  overflow-wrap: anywhere;
  color: var(--ac-text-muted);
  font-family: 'Fira Code', 'JetBrains Mono', monospace;
  font-size: 11px;
  line-height: 1.5;
}
</style>
