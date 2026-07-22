<template>
  <q-page class="surface-page">
    <NewSessionDialog
      page
      :model-value="true"
      :default-project-id="defaultProjectId"
      @update:model-value="close"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import NewSessionDialog from '@/components/NewSessionDialog.vue';

const route = useRoute();
const router = useRouter();
const defaultProjectId = computed(() =>
  typeof route.query.projectId === 'string' ? route.query.projectId : '',
);

function close() {
  void router.push({ name: 'overview' });
}
</script>
