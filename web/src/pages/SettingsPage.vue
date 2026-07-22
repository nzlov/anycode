<template>
  <q-page class="surface-page">
    <GlobalSettingsDialog page :model-value="true" @update:model-value="close" />
  </q-page>
</template>

<script setup lang="ts">
import { useRouter } from 'vue-router';

import GlobalSettingsDialog from '@/components/GlobalSettingsDialog.vue';

const router = useRouter();

function close() {
  void router.push({ name: 'overview' });
}
</script>
