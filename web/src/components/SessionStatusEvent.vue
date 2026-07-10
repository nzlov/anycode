<template>
  <div class="status-event">
    <q-icon name="info_outline" size="16px" />
    <div class="status-event__content">
      <strong>{{ event.title }}</strong>
      <span v-if="event.body">{{ event.body }}</span>
    </div>
    <time v-if="event.time">{{ event.time }}</time>
  </div>
</template>

<script setup lang="ts">
import type { SessionEvent } from '@/services/sessions';

defineProps<{ event: SessionEvent }>();
</script>

<style scoped>
.status-event {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  align-items: start;
  gap: 8px;
  padding: 5px 8px;
  color: var(--ac-text-muted);
  font-size: 13px;
}

.status-event__content {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  gap: 4px 8px;
}

.status-event__content strong {
  color: var(--ac-text);
  font-weight: 600;
}

.status-event__content span {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.status-event time {
  font-size: 12px;
}
</style>
