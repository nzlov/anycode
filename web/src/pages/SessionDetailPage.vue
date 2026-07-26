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
    :mind-map-available="mindMapAvailable"
    :mind-map-realtime="mindMapRealtime"
    page
    @session-title="emit('session-title', $event)"
  />
  <q-page v-else class="flex flex-center">
    <q-spinner color="primary" size="32px" />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';

import SessionDetailView from '@/components/SessionDetailView.vue';
import TerminalSessionView from '@/components/TerminalSessionView.vue';
import { useGeneralSettingsInvalidation } from '@/composables/useGeneralSettingsInvalidation';
import { getGeneralSettings } from '@/services/generalSettings';
import { listProjects } from '@/services/projects';
import { getSession, type SessionMode } from '@/services/sessions';

const emit = defineEmits<{
  'session-title': [title: string];
}>();
const route = useRoute();
const sessionId = computed(() => String(route.params.id ?? ''));
const mode = ref<SessionMode | ''>('');
const mindMapAvailable = ref(false);
const mindMapRealtime = ref(false);
const sessionProjectId = ref('');
const generalSettingsInvalidation = useGeneralSettingsInvalidation();

if (generalSettingsInvalidation) {
  watch(generalSettingsInvalidation.revision, refreshMindMapMode);
}

onMounted(async () => {
  const session = await getSession(sessionId.value);
  mode.value = session.mode;
  sessionProjectId.value = session.projectId;
  emit('session-title', session.title);
  if (session.mode === 'terminal') return;
  await refreshMindMapMode();
});

async function refreshMindMapMode() {
  if (!sessionProjectId.value || mode.value === 'terminal') return;
  const [settings, projects] = await Promise.all([
    getGeneralSettings().catch(() => null),
    listProjects().catch(() => []),
  ]);
  mindMapAvailable.value =
    Boolean(settings?.mindMapEnabled) &&
    projects.some((project) => project.id === sessionProjectId.value && project.mindMapEnabled);
  mindMapRealtime.value = mindMapAvailable.value && settings?.mindMapMode === 'realtime';
}
</script>
