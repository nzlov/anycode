<template>
  <q-page class="surface-page surface-page--wide">
    <q-card flat class="surface-page__card">
      <q-card-section class="surface-page__header">
        <div class="text-subtitle1 text-weight-bold">临时文件</div>
      </q-card-section>
      <q-separator />
      <q-card-section class="surface-page__body">
        <SessionArtifactsPanel :session-id="sessionId" />
      </q-card-section>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute } from 'vue-router';

import SessionArtifactsPanel from '@/components/SessionArtifactsPanel.vue';

const route = useRoute();
const sessionId = computed(() => String(route.params.id ?? ''));
</script>
