<template>
  <q-page class="surface-page surface-page--wide">
    <q-card flat class="surface-page__card">
      <q-card-section class="surface-page__header">
        <div class="text-subtitle1 text-weight-bold ellipsis">{{ file?.filename || '文件预览' }}</div>
        <q-btn
          v-if="file"
          flat
          round
          dense
          class="app-icon-btn"
          icon="download"
          aria-label="下载文件"
          :loading="downloading"
          @click="download"
        />
      </q-card-section>
      <q-separator />
      <q-card-section class="surface-page__body">
        <q-spinner v-if="loading" color="primary" size="32px" />
        <q-banner v-else-if="error" dense class="text-negative">{{ error }}</q-banner>
        <SessionFilePreview v-else-if="file" :file="file" />
      </q-card-section>
    </q-card>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { Notify } from 'quasar';
import { useRoute } from 'vue-router';

import SessionFilePreview from '@/components/SessionFilePreview.vue';
import {
  downloadSessionFile,
  listSessionFiles,
  type SessionFile,
} from '@/services/sessionFiles';

const route = useRoute();
const sessionId = computed(() => String(route.params.id ?? ''));
const fileId = computed(() => String(route.params.fileId ?? ''));
const file = ref<SessionFile | null>(null);
const loading = ref(true);
const downloading = ref(false);
const error = ref('');

onMounted(async () => {
  try {
    const files = await listSessionFiles({ sessionId: sessionId.value });
    file.value = files.find((item) => item.id === fileId.value) ?? null;
    if (!file.value) error.value = '文件已不存在';
  } catch (err) {
    error.value = err instanceof Error ? err.message : '读取文件失败';
  } finally {
    loading.value = false;
  }
});

async function download() {
  if (!file.value) return;
  downloading.value = true;
  try {
    await downloadSessionFile(file.value);
  } catch (err) {
    Notify.create({ type: 'negative', message: err instanceof Error ? err.message : '下载文件失败' });
  } finally {
    downloading.value = false;
  }
}
</script>
