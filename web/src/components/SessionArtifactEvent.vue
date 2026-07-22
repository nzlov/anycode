<template>
  <div class="artifact-event">
    <q-icon :name="icon" size="22px" :class="deleted ? 'text-muted' : 'text-primary'" />
    <div class="artifact-event__content">
      <div class="artifact-event__name" :class="{ 'text-strike text-muted': deleted }">
        {{ filename }}
      </div>
      <div class="text-caption text-muted">
        {{ deleted ? '已删除' : `${formatBytes(size)} · ${artifactKind}` }}
      </div>
    </div>
    <div v-if="!deleted" class="artifact-event__actions">
      <q-btn
        v-if="previewKind !== 'none' && previewUrl"
        flat
        round
        dense
        icon="visibility"
        aria-label="预览临时文件"
        @click="openPreview"
      >
        <q-tooltip>预览</q-tooltip>
      </q-btn>
      <q-btn
        flat
        round
        dense
        icon="download"
        aria-label="下载临时文件"
        :loading="downloading"
        @click="download"
      >
        <q-tooltip>下载</q-tooltip>
      </q-btn>
    </div>

    <q-dialog v-model="previewOpen" @hide="clearPreview">
      <q-card class="artifact-event-preview app-content-dialog">
        <q-card-section class="artifact-event-preview__header">
          <span>{{ filename }}</span>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" />
        </q-card-section>
        <q-separator />
        <q-card-section class="artifact-event-preview__body">
          <q-spinner v-if="previewLoading" color="primary" size="32px" />
          <q-banner v-else-if="previewError" dense class="text-negative">{{
            previewError
          }}</q-banner>
          <img v-else-if="previewKind === 'image' && objectUrl" :src="objectUrl" :alt="filename" />
          <iframe
            v-else-if="previewKind === 'pdf' && objectUrl"
            :src="objectUrl"
            title="PDF 预览"
          />
          <video v-else-if="previewKind === 'video' && objectUrl" :src="objectUrl" controls />
          <audio v-else-if="previewKind === 'audio' && objectUrl" :src="objectUrl" controls />
          <pre v-else-if="previewKind === 'text'">{{ text }}</pre>
        </q-card-section>
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import { Notify, useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import { downloadSessionFile, fetchSessionFile } from '@/services/sessionFiles';
import type { TranscriptItem, TranscriptUnknownContent } from '@/services/sessionTimeline';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptUnknownContent };
}>();
const $q = useQuasar();
const route = useRoute();
const router = useRouter();
const payload = computed(() => props.event.content.payload);
const filename = computed(
  () => payloadString('filename') || payloadString('logicalPath') || '临时文件',
);
const artifactKind = computed(() => payloadString('artifactKind', 'file'));
const previewKind = computed(() => payloadString('previewKind', 'none'));
const previewUrl = computed(() => payloadString('previewUrl'));
const downloadUrl = computed(() => payloadString('downloadUrl'));
const size = computed(() => Number(payload.value.size || 0));
const deleted = computed(() => payload.value.status === 'deleted');
const downloading = ref(false);
const previewOpen = ref(false);
const previewLoading = ref(false);
const previewError = ref('');
const objectUrl = ref('');
const text = ref('');
let previewController: AbortController | null = null;
const icon = computed(() => {
  const icons: Record<string, string> = {
    image: 'image',
    pdf: 'picture_as_pdf',
    video: 'movie',
    audio: 'audio_file',
    archive: 'folder_zip',
    text: 'description',
  };
  return icons[artifactKind.value] ?? 'draft';
});

const access = computed(() => ({
  filename: filename.value,
  previewUrl: previewUrl.value || null,
  downloadUrl: downloadUrl.value,
}));

async function download() {
  downloading.value = true;
  try {
    await downloadSessionFile(access.value);
  } catch (err) {
    Notify.create({ type: 'negative', message: errorMessage(err, '下载临时文件失败') });
  } finally {
    downloading.value = false;
  }
}

async function openPreview() {
  const fileId = payloadString('id');
  const sessionId = String(route.params.id ?? '');
  if ($q.screen.lt.sm && fileId && sessionId) {
    await router.push({ name: 'session-artifact', params: { id: sessionId, fileId } });
    return;
  }
  clearPreview();
  previewOpen.value = true;
  previewLoading.value = true;
  previewError.value = '';
  const controller = new AbortController();
  previewController = controller;
  try {
    if (previewKind.value === 'text' && size.value > 1 << 20) {
      throw new Error('文本超过 1 MiB，请下载查看');
    }
    const blob = await fetchSessionFile(access.value, 'preview', controller.signal);
    if (previewController !== controller) return;
    if (previewKind.value === 'text') {
      const content = await blob.text();
      if (previewController === controller) text.value = content;
    } else {
      objectUrl.value = URL.createObjectURL(blob);
    }
  } catch (err) {
    if (!isAbortError(err) && previewController === controller) {
      previewError.value = errorMessage(err, '预览临时文件失败');
    }
  } finally {
    if (previewController === controller) {
      previewController = null;
      previewLoading.value = false;
    }
  }
}

function clearPreview() {
  previewController?.abort();
  previewController = null;
  previewLoading.value = false;
  if (objectUrl.value) URL.revokeObjectURL(objectUrl.value);
  objectUrl.value = '';
  text.value = '';
  previewError.value = '';
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1 << 20) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1 << 20)).toFixed(1)} MiB`;
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback;
}

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

function payloadString(key: string, fallback = '') {
  const value = payload.value[key];
  return typeof value === 'string' ? value : fallback;
}

onBeforeUnmount(clearPreview);
</script>

<style scoped>
.artifact-event,
.artifact-event__actions,
.artifact-event-preview__header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.artifact-event {
  padding: 10px 12px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-raised);
}

.artifact-event__content {
  min-width: 0;
  flex: 1 1 auto;
}

.artifact-event__name,
.artifact-event-preview__header span {
  overflow-wrap: anywhere;
  word-break: break-word;
}

.artifact-event-preview {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.artifact-event-preview__header {
  justify-content: space-between;
  font-weight: 600;
}

.artifact-event-preview__body {
  display: grid;
  min-height: 260px;
  flex: 1 1 auto;
  place-items: center;
  overflow: auto;
  background: var(--ac-surface-muted);
}

.artifact-event-preview__body img,
.artifact-event-preview__body video {
  max-width: 100%;
  max-height: 72vh;
  object-fit: contain;
}

.artifact-event-preview__body iframe {
  width: 100%;
  min-height: 68vh;
  border: 0;
}

.artifact-event-preview__body audio {
  width: min(100%, 520px);
}

.artifact-event-preview__body pre {
  width: 100%;
  margin: 0;
  align-self: start;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
