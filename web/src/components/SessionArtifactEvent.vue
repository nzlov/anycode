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

    <q-dialog v-model="previewOpen" :maximized="$q.screen.lt.md" @hide="clearPreview">
      <q-card
        class="artifact-event-preview"
        :class="{
          'app-content-dialog': !$q.screen.lt.md,
          'artifact-event-preview--mobile': $q.screen.lt.md,
        }"
      >
        <q-card-section v-if="!$q.screen.lt.md" class="artifact-event-preview__header">
          <span>{{ filename }}</span>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" />
        </q-card-section>
        <q-separator v-if="!$q.screen.lt.md" />
        <SessionFilePreview
          :file="selectedPreview"
          :zoomable="$q.screen.lt.md"
          :annotation-source="`临时文件 ${filename}`"
        />
        <q-btn
          v-if="$q.screen.lt.md"
          v-close-popup
          round
          dense
          class="artifact-event-preview__close"
          icon="close"
          aria-label="关闭"
        />
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { Notify, useQuasar } from 'quasar';

import SessionFilePreview from '@/components/SessionFilePreview.vue';
import {
  downloadSessionFile,
  type SessionFilePreviewData,
  type SessionFilePreviewKind,
} from '@/services/sessionFiles';
import type { TranscriptItem, TranscriptUnknownContent } from '@/services/sessionTimeline';

const props = defineProps<{
  event: TranscriptItem & { content: TranscriptUnknownContent };
}>();
const $q = useQuasar();
const payload = computed(() => props.event.content.payload);
const filename = computed(
  () => payloadString('filename') || payloadString('logicalPath') || '临时文件',
);
const artifactKind = computed(() => payloadString('artifactKind', 'file'));
const previewKind = computed<SessionFilePreviewKind>(() => {
  const kind = payloadString('previewKind', 'none');
  return ['image', 'pdf', 'video', 'audio', 'model', 'text'].includes(kind)
    ? (kind as SessionFilePreviewKind)
    : 'none';
});
const previewUrl = computed(() => payloadString('previewUrl'));
const downloadUrl = computed(() => payloadString('downloadUrl'));
const size = computed(() => Number(payload.value.size || 0));
const deleted = computed(() => payload.value.status === 'deleted');
const downloading = ref(false);
const previewOpen = ref(false);
const selectedPreview = ref<SessionFilePreviewData | null>(null);
const icon = computed(() => {
  const icons: Record<string, string> = {
    image: 'image',
    pdf: 'picture_as_pdf',
    video: 'movie',
    audio: 'audio_file',
    model: 'view_in_ar',
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
const previewFile = computed<SessionFilePreviewData>(() => ({
  id: payloadString('id') || props.event.id,
  ...access.value,
  size: size.value,
  previewKind: previewKind.value,
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

function openPreview() {
  selectedPreview.value = previewFile.value;
  previewOpen.value = true;
}

function clearPreview() {
  selectedPreview.value = null;
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1 << 20) return `${(value / 1024).toFixed(1)} KiB`;
  return `${(value / (1 << 20)).toFixed(1)} MiB`;
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback;
}

function payloadString(key: string, fallback = '') {
  const value = payload.value[key];
  return typeof value === 'string' ? value : fallback;
}
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
  position: relative;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.artifact-event-preview--mobile {
  width: 100%;
  max-width: none;
  height: 100%;
  max-height: none;
  border-radius: 0;
}

.artifact-event-preview--mobile :deep(.session-file-preview) {
  min-height: 0;
  flex: 1 1 auto;
}

.artifact-event-preview--mobile :deep(.session-file-preview__image),
.artifact-event-preview--mobile :deep(.session-file-preview__media) {
  max-height: 100%;
}

.artifact-event-preview__close {
  position: absolute;
  z-index: 1;
  top: max(12px, env(safe-area-inset-top));
  right: max(12px, env(safe-area-inset-right));
  color: var(--ac-text);
  background: color-mix(in srgb, var(--ac-surface) 88%, transparent);
  box-shadow: var(--ac-shadow-card);
}

.artifact-event-preview__header {
  justify-content: space-between;
  font-weight: 600;
}
</style>
