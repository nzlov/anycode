<template>
  <div class="usage-event">
    <div class="usage-event__header">
      <q-icon name="data_usage" size="16px" />
      <strong>{{ event.title }}</strong>
      <time v-if="event.time">{{ event.time }}</time>
    </div>
    <div v-if="event.usage" class="usage-event__metrics">
      <span>输入 {{ formatNumber(event.usage.inputTokens) }}</span>
      <span>缓存 {{ formatNumber(event.usage.cachedInputTokens) }}</span>
      <span>输出 {{ formatNumber(event.usage.outputTokens) }}</span>
      <span>推理 {{ formatNumber(event.usage.reasoningOutputTokens) }}</span>
      <span>累计 {{ formatNumber(event.usage.totalTokens) }}</span>
      <span v-if="event.usage.contextWindow"
        >上下文 {{ formatNumber(event.usage.contextWindow) }}</span
      >
    </div>
  </div>
</template>

<script setup lang="ts">
import type { SessionEvent } from '@/services/sessions';

defineProps<{ event: SessionEvent }>();

function formatNumber(value: number) {
  return value.toLocaleString();
}
</script>

<style scoped>
.usage-event {
  padding: 7px 10px;
  border-left: 2px solid var(--q-info);
  background: var(--ac-surface-muted);
  color: var(--ac-text-muted);
  font-size: 12px;
}

.usage-event__header {
  display: flex;
  align-items: center;
  gap: 7px;
}

.usage-event__header strong {
  flex: 1 1 auto;
  color: var(--ac-text);
  font-size: 13px;
}

.usage-event__metrics {
  display: flex;
  flex-wrap: wrap;
  gap: 5px 14px;
  margin-top: 5px;
}
</style>
