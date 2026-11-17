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

<style scoped>
.surface-page {
  height: calc(100dvh - 50px);
  min-height: 0 !important;
  overflow: hidden;
}
.surface-page__card {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
}
.surface-page__body {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
}
</style>
