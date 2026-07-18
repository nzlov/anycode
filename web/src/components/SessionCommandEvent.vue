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
      <span class="command-event__title">{{ title }}</span>
      <q-badge
        v-if="singleExitCode !== null"
        outline
        :color="singleExitCode === 0 ? 'positive' : 'negative'"
        :label="`exit ${singleExitCode}`"
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
      <section
        v-for="(command, index) in content.commands"
        :key="index"
        class="command-event__invocation"
      >
        <div v-if="content.commands.length > 1" class="command-event__input-label">
          命令 {{ index + 1 }}
        </div>
        <div class="command-event__input">
          <div class="command-event__label">输入</div>
          <pre class="command-event__command"><code>{{ command.command }}</code></pre>
          <div v-if="command.workdir" class="command-event__workdir">{{ command.workdir }}</div>
        </div>
        <div v-if="command.hasOutput" class="command-event__result">
          <div class="command-event__result-header">
            <div class="command-event__label">输出</div>
            <q-badge
              v-if="command.exitCode !== null"
              outline
              :color="command.exitCode === 0 ? 'positive' : 'negative'"
              :label="`exit ${command.exitCode}`"
            />
            <small v-if="formatDuration(command.durationMs)">
              {{ formatDuration(command.durationMs) }}
            </small>
          </div>
          <StaticAnsiOutput :text="command.output" appearance="surface" />
        </div>
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
const title = computed(() => (content.value.kind === 'exec' ? 'Exec' : 'Shell'));
const singleExitCode = computed(() =>
  content.value.commands.length === 1 ? (content.value.commands[0]?.exitCode ?? null) : null,
);
const canExpand = computed(() => content.value.commands.length > 0);
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
  gap: 12px;
  margin-top: 6px;
}

.command-event__invocation,
.command-event__input,
.command-event__result {
  display: grid;
  min-width: 0;
  gap: 6px;
}

.command-event__invocation + .command-event__invocation {
  padding-top: 10px;
  border-top: 1px solid var(--ac-border);
}

.command-event__label {
  color: var(--ac-text-muted);
  font-size: 12px;
  font-weight: 600;
}

.command-event__input-label {
  margin-bottom: 4px;
  color: var(--ac-text-muted);
  font-size: 11px;
}

.command-event__result-header {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
}

.command-event__result-header small {
  color: var(--ac-text-muted);
  font-size: 11px;
}

.command-event__command {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  max-width: 100%;
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
