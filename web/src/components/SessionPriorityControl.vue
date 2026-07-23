<template>
  <q-btn
    flat
    dense
    no-caps
    class="session-priority-control"
    icon="flag"
    :label="priorityLabel(priority)"
    :loading="loading"
    :disable="disabled"
    aria-label="变更会话优先级"
    @click.stop
  >
    <q-menu>
      <q-list dense class="app-touch-list">
        <q-item-label header>优先级</q-item-label>
        <q-item
          v-for="option in priorities"
          :key="option"
          v-close-popup
          clickable
          :active="priority === option"
          @click.stop="emit('change', option)"
        >
          <q-item-section>{{ priorityLabel(option) }}</q-item-section>
          <q-item-section v-if="priority === option" side>
            <q-icon name="check" color="primary" />
          </q-item-section>
        </q-item>
      </q-list>
    </q-menu>
    <q-tooltip>变更会话优先级</q-tooltip>
  </q-btn>
</template>

<script setup lang="ts">
import {
  sessionPriorities as priorities,
  sessionPriorityLabel as priorityLabel,
} from '@/services/sessionPriorityPresentation';
import type { SessionPriority } from '@/services/sessions';

defineProps<{
  priority: SessionPriority;
  loading?: boolean;
  disabled?: boolean;
}>();

const emit = defineEmits<{
  change: [priority: SessionPriority];
}>();

</script>

<style scoped>
.session-priority-control {
  min-height: 24px;
  padding: 0 6px;
  color: var(--ac-text-muted);
  font-size: 12px;
}
</style>
