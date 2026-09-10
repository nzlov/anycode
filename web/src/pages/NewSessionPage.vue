<template>
  <q-page class="new-session-page">
    <PageToolbar title="新建卡片" />
    <NewSessionDialog
      class="new-session-page__form"
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
import PageToolbar from '@/components/PageToolbar.vue';

const route = useRoute();
const router = useRouter();
const defaultProjectId = computed(() =>
  typeof route.query.projectId === 'string' ? route.query.projectId : '',
);

function close() {
  void router.push({ name: 'overview' });
}
</script>

<style scoped lang="scss">
.new-session-page {
  display: flex;
  flex-direction: column;
  background: var(--ac-surface);
}

.new-session-page__form {
  display: flex;
  min-width: 0;
  flex: 1 1 auto;
}

.new-session-page :deep(.new-session-page-content) {
  flex: 1 1 auto;
}

.new-session-page :deep(.new-session-body) {
  display: flex;
  flex-direction: column;
}

.new-session-page :deep(.prompt-shell) {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  border-right: 0;
  border-bottom: 0;
  border-left: 0;
  border-radius: 0;
}

.new-session-page :deep(.prompt-input-wrap) {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
}

.new-session-page :deep(.prompt-input) {
  display: flex;
  flex: 1 1 auto;
}

.new-session-page :deep(.prompt-input .q-field__control) {
  height: 100%;
}
</style>
