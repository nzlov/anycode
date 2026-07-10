<template>
  <div class="tool-event">
    <button
      type="button"
      class="tool-event__header"
      :aria-expanded="expanded"
      @click="expanded = !expanded"
    >
      <q-icon :name="expanded ? 'expand_more' : 'chevron_right'" size="18px" />
      <q-icon :name="isExec ? 'terminal' : 'build'" size="16px" />
      <span>{{ displayTitle }}</span>
      <time>{{ event.time }}</time>
    </button>
    <template v-if="expanded">
      <div v-if="isExec" class="tool-event__exec">
        <section class="tool-event__terminal">
          <div class="tool-event__terminal-label">输入</div>
          <SessionTerminalOutput :body="event.execInput ?? ''" />
        </section>
        <section class="tool-event__terminal">
          <div class="tool-event__terminal-label">输出</div>
          <SessionTerminalOutput :body="event.execOutput ?? ''" />
        </section>
      </div>
      <SessionTerminalOutput v-else-if="event.body" :body="event.body" />
      <SessionEventImages :event-id="event.id" :images="event.images ?? []" label="工具输出图片" />
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import SessionEventImages from '@/components/SessionEventImages.vue';
import SessionTerminalOutput from '@/components/SessionTerminalOutput.vue';
import type { SessionEvent } from '@/services/sessions';

const props = defineProps<{ event: SessionEvent }>();
const expanded = ref(false);
const isExec = computed(
  () => props.event.execInput !== undefined || props.event.execOutput !== undefined,
);
const displayTitle = computed(() => {
  if (!props.event.command || props.event.title.startsWith('Shell ')) return props.event.title;
  return `Shell ${props.event.title}`;
});
</script>

<style scoped>
.tool-event__exec {
  display: grid;
  gap: 8px;
  margin-top: 6px;
}

.tool-event__terminal {
  min-width: 0;
}

.tool-event__terminal-label {
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
