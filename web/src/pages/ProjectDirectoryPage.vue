<template>
  <q-page class="surface-page">
    <ProjectDirectoryDialog page :model-value="true" @update:model-value="close" />
  </q-page>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';

import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';

const router = useRouter();

function close() {
  void router.push({ name: 'overview' });
}
</script>
