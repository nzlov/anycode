<template>
  <q-menu context-menu @before-show="emit('before-show', $event)" @click.stop>
    <q-list dense class="overview-card-menu app-touch-list">
      <q-item
        v-close-popup
        clickable
        tag="a"
        :href="router.resolve({ name: 'session-detail', params: { id: card.id } }).href"
        target="_blank"
        rel="noopener noreferrer"
        @click.stop
      >
        <q-item-section avatar>
          <q-icon name="open_in_new" />
        </q-item-section>
        <q-item-section>在新标签页中打开</q-item-section>
      </q-item>
      <q-separator />
      <q-item-label header>优先级</q-item-label>
      <q-item
        v-for="priority in priorities"
        :key="priority"
        v-close-popup
        clickable
        :active="card.priority === priority"
        :disable="card.status === 'closed' || priorityLoading"
        @click.stop="emit('set-priority', priority)"
      >
        <q-item-section>{{ priorityLabel(priority) }}</q-item-section>
        <q-item-section v-if="card.priority === priority" side>
          <q-icon name="check" color="primary" />
        </q-item-section>
      </q-item>
      <q-separator />
      <q-item
        v-close-popup
        clickable
        class="text-negative"
        :disable="!card.availableActions.includes('close') || closeLoading"
        @click.stop="emit('close')"
      >
        <q-item-section avatar>
          <q-icon name="close" />
        </q-item-section>
        <q-item-section>关闭卡片</q-item-section>
      </q-item>
    </q-list>
  </q-menu>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';

import {
  sessionPriorities as priorities,
  sessionPriorityLabel as priorityLabel,
} from '@/services/sessionPriorityPresentation';
import type { SessionCard, SessionPriority } from '@/services/sessions';

defineProps<{
  card: SessionCard;
  priorityLoading?: boolean;
  closeLoading?: boolean;
}>();

const emit = defineEmits<{
  'before-show': [event: Event];
  'set-priority': [priority: SessionPriority];
  close: [];
}>();

const router = useRouter();
</script>
