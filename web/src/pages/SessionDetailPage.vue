<template>
  <TerminalSessionView
    v-if="mode === 'terminal'"
    :session-id="sessionId"
    @session-title="emit('session-title', $event)"
  />
  <SessionDetailView
    v-else-if="mode"
    :session-id="sessionId"
    layout="responsive"
    page
    @session-title="emit('session-title', $event)"
  />
  <q-page v-else class="flex flex-center">
    <q-spinner color="primary" size="32px" />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';

import SessionDetailView from '@/components/SessionDetailView.vue';
import TerminalSessionView from '@/components/TerminalSessionView.vue';
import { getSession, type SessionMode } from '@/services/sessions';

const emit = defineEmits<{
  'session-title': [title: string];
}>();
const route = useRoute();
const sessionId = computed(() => String(route.params.id ?? ''));
const mode = ref<SessionMode | ''>('');

onMounted(async () => {
  const session = await getSession(sessionId.value);
  mode.value = session.mode;
  emit('session-title', session.title);
});
</script>
