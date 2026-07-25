<template>
  <div class="session-file-preview">
    <q-spinner v-if="loading" color="primary" size="32px" />
    <q-banner v-else-if="error" dense class="session-file-preview__error">{{ error }}</q-banner>
    <div
      v-else-if="file?.previewKind === 'image' && objectURL"
      class="session-file-preview__zoom-surface"
      :class="{ 'session-file-preview__zoom-surface--enabled': zoomable }"
      @pointerdown="startZoom"
      @pointermove="moveZoom"
      @pointerup="endZoom"
      @pointercancel="endZoom"
    >
      <img
        :src="objectURL"
        :alt="file.filename"
        class="session-file-preview__image"
        :style="mediaTransform"
      />
    </div>
    <iframe
      v-else-if="file?.previewKind === 'pdf' && objectURL"
      :src="objectURL"
      class="session-file-preview__frame"
      title="PDF 预览"
    />
    <div
      v-else-if="file?.previewKind === 'video' && objectURL"
      class="session-file-preview__zoom-surface"
      :class="{ 'session-file-preview__zoom-surface--enabled': zoomable }"
      @pointerdown="startZoom"
      @pointermove="moveZoom"
      @pointerup="endZoom"
      @pointercancel="endZoom"
    >
      <video
        :src="objectURL"
        class="session-file-preview__media"
        :style="mediaTransform"
        controls
      />
    </div>
    <audio
      v-else-if="file?.previewKind === 'audio' && objectURL"
      :src="objectURL"
      class="session-file-preview__audio"
      controls
    />
    <pre v-else-if="file?.previewKind === 'text'" class="session-file-preview__text">{{
      text
    }}</pre>
    <div v-else class="session-file-preview__state text-muted">
      <q-icon :name="file ? 'draft' : 'inventory_2'" size="36px" />
      <span>{{ file ? '此文件仅支持下载' : '暂无临时文件' }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { fetchSessionFile, type SessionFilePreviewData } from '@/services/sessionFiles';

const props = withDefaults(
  defineProps<{ file: SessionFilePreviewData | null; zoomable?: boolean }>(),
  { zoomable: false },
);
const loading = ref(false);
const error = ref('');
const objectURL = ref('');
const text = ref('');
const scale = ref(1);
const pointers = new Map<number, { x: number; y: number }>();
let pinchStartDistance = 0;
let pinchStartScale = 1;
let controller: AbortController | null = null;
const mediaTransform = computed(() =>
  props.zoomable ? { transform: `scale(${scale.value})` } : undefined,
);

async function load(file: SessionFilePreviewData | null) {
  clear();
  if (!file || file.previewKind === 'none') return;
  if (file.previewKind === 'text' && file.size > 1 << 20) {
    error.value = '文本超过 1 MiB，请下载查看';
    return;
  }
  const request = new AbortController();
  controller = request;
  loading.value = true;
  try {
    const blob = await fetchSessionFile(file, 'preview', request.signal);
    if (controller !== request || props.file?.id !== file.id) return;
    if (file.previewKind === 'text') {
      const content = await blob.text();
      if (controller === request && props.file?.id === file.id) text.value = content;
    } else {
      objectURL.value = URL.createObjectURL(blob);
    }
  } catch (err) {
    if (!isAbortError(err) && controller === request) {
      error.value = errorMessage(err, '预览文件失败');
    }
  } finally {
    if (controller === request) {
      controller = null;
      loading.value = false;
    }
  }
}

function clear() {
  controller?.abort();
  controller = null;
  loading.value = false;
  if (objectURL.value) URL.revokeObjectURL(objectURL.value);
  objectURL.value = '';
  text.value = '';
  error.value = '';
  resetZoom();
}

function startZoom(event: PointerEvent) {
  if (!props.zoomable || event.pointerType !== 'touch') return;
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (pointers.size === 2) {
    pinchStartDistance = pointerDistance();
    pinchStartScale = scale.value;
  }
}

function moveZoom(event: PointerEvent) {
  if (!props.zoomable || !pointers.has(event.pointerId)) return;
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (pointers.size !== 2 || pinchStartDistance === 0) return;
  scale.value = Math.min(4, Math.max(1, pinchStartScale * (pointerDistance() / pinchStartDistance)));
}

function endZoom(event: PointerEvent) {
  pointers.delete(event.pointerId);
  pinchStartDistance = 0;
  pinchStartScale = scale.value;
}

function pointerDistance() {
  const [first, second] = [...pointers.values()];
  if (!first || !second) return 0;
  return Math.hypot(second.x - first.x, second.y - first.y);
}

function resetZoom() {
  pointers.clear();
  pinchStartDistance = 0;
  pinchStartScale = 1;
  scale.value = 1;
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error && err.message ? err.message : fallback;
}

function isAbortError(err: unknown) {
  return err instanceof DOMException && err.name === 'AbortError';
}

watch(() => props.file, load, { immediate: true });
onBeforeUnmount(clear);
</script>

<style scoped>
.session-file-preview {
  display: grid;
  min-width: 0;
  min-height: 260px;
  height: 100%;
  place-items: center;
  overflow: auto;
  background: var(--ac-surface-muted);
}

.session-file-preview__error {
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
  color: var(--ac-on-error-container);
  background: var(--ac-error-container);
}

.session-file-preview__state {
  display: grid;
  place-items: center;
  gap: 8px;
}

.session-file-preview__image,
.session-file-preview__media {
  display: block;
  max-width: 100%;
  max-height: 72vh;
  object-fit: contain;
  transform-origin: center;
}

.session-file-preview__zoom-surface {
  display: grid;
  width: 100%;
  height: 100%;
  place-items: center;
  overflow: hidden;
}

.session-file-preview__zoom-surface--enabled {
  touch-action: none;
}

.session-file-preview__frame {
  width: 100%;
  min-height: 68vh;
  border: 0;
}

.session-file-preview__audio {
  width: min(100%, 520px);
}

.session-file-preview__text {
  width: 100%;
  margin: 0;
  align-self: start;
  overflow: auto;
  color: var(--ac-text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>
