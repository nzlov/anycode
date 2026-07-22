<template>
  <q-page class="surface-page">
    <ProjectSettingsDialog
      page
      :model-value="true"
      :project="project"
      @update:model-value="close"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import ProjectSettingsDialog from '@/components/ProjectSettingsDialog.vue';
import { useProjects } from '@/composables/useProjects';

const route = useRoute();
const router = useRouter();
const { projects, loadProjects } = useProjects();
const projectId = computed(() => String(route.params.projectId ?? ''));
const project = computed(() => projects.value.find((item) => item.id === projectId.value) ?? null);

onMounted(() => void loadProjects());

function close() {
  void router.push({ name: 'settings' });
}
</script>
