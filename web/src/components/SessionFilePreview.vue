<template>
  <div class="session-file-preview">
    <div
      v-if="file?.previewKind === 'image'"
      ref="zoomSurface"
      class="session-file-preview__zoom-surface"
      :class="{ 'session-file-preview__zoom-surface--enabled': zoomable }"
      @pointerdown="startGesture"
      @pointermove="moveGesture"
      @pointerup="endGesture"
      @pointercancel="endGesture"
    >
      <PreviewAnnotator
        mode="image"
        :enabled="annotatable && !annotationReadOnly"
        :source="annotationSource || `临时文件 ${file.filename}`"
        :session-id="annotationSessionId"
        :content-key="file.id"
        :file-references="[{ kind: 'session_file', sessionFileId: file.id }]"
        :display-annotations="displayAnnotations"
      >
        <template #toolbar-leading><slot name="toolbar-leading" /></template>
        <template #toolbar-actions><slot name="toolbar-actions" /></template>
        <q-banner v-if="error" dense class="session-file-preview__error">{{ error }}</q-banner>
        <img
          v-else-if="imageURL"
          ref="mediaElement"
          :src="imageURL"
          :alt="file.filename"
          class="session-file-preview__image"
          :style="mediaTransform"
          @load="finishImageLoad($event)"
          @error="failImageLoad($event)"
        />
        <q-spinner
          v-if="loading"
          class="session-file-preview__loading"
          color="primary"
          size="32px"
        />
      </PreviewAnnotator>
    </div>
    <PreviewAnnotator
      v-else-if="file?.previewKind === 'text'"
      mode="text"
      :enabled="annotatable && !annotationReadOnly"
      :source="annotationSource || `临时文件 ${file.filename}`"
      :session-id="annotationSessionId"
      :content-key="file.id"
      :file-references="[{ kind: 'session_file', sessionFileId: file.id }]"
      :display-annotations="displayAnnotations"
    >
      <template #toolbar-leading><slot name="toolbar-leading" /></template>
      <template #toolbar-actions><slot name="toolbar-actions" /></template>
      <q-banner v-if="error" dense class="session-file-preview__error">{{ error }}</q-banner>
      <div v-else-if="loading" class="session-file-preview__state">
        <q-spinner color="primary" size="32px" />
      </div>
      <pre
        v-else
        class="session-file-preview__text"
        data-annotation-text
        data-annotation-line="1"
        >{{ text }}</pre>
    </PreviewAnnotator>
    <q-banner v-else-if="error" dense class="session-file-preview__error">{{ error }}</q-banner>
    <q-spinner v-else-if="loading" color="primary" size="32px" />
    <iframe
      v-else-if="file?.previewKind === 'pdf' && objectURL"
      :src="objectURL"
      class="session-file-preview__frame"
      title="PDF 预览"
    />
    <div
      v-else-if="file?.previewKind === 'video' && objectURL"
      ref="zoomSurface"
      class="session-file-preview__zoom-surface"
      :class="{ 'session-file-preview__zoom-surface--enabled': zoomable }"
      @pointerdown="startGesture"
      @pointermove="moveGesture"
      @pointerup="endGesture"
      @pointercancel="endGesture"
    >
      <video
        ref="mediaElement"
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
    <ModelFilePreview
      v-else-if="file?.previewKind === 'model' && objectURL"
      :src="objectURL"
      :filename="file.filename"
      class="session-file-preview__model"
    />
    <div v-else class="session-file-preview__state text-muted">
      <q-icon :name="file ? 'draft' : 'inventory_2'" size="36px" />
      <span>{{ file ? '此文件仅支持下载' : '暂无临时文件' }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue';

import PreviewAnnotator from '@/components/PreviewAnnotator.vue';
import type { PreviewAnnotation } from '@/services/previewAnnotations';
import {
  fetchSessionFile,
  requestSessionFilePreviewURL,
  type SessionFilePreviewData,
} from '@/services/sessionFiles';

const ModelFilePreview = defineAsyncComponent(() => import('@/components/ModelFilePreview.vue'));

const props = withDefaults(
  defineProps<{
    file: SessionFilePreviewData | null;
    zoomable?: boolean;
    annotationSource?: string;
    annotationSessionId?: string;
    annotatable?: boolean;
    annotationReadOnly?: boolean;
    displayAnnotations?: PreviewAnnotation[];
  }>(),
  {
    zoomable: false,
    annotationSource: '',
    annotationSessionId: '',
    annotatable: true,
    annotationReadOnly: false,
    displayAnnotations: () => [],
  },
);
const loading = ref(false);
const error = ref('');
const imageURL = ref('');
const objectURL = ref('');
const text = ref('');
const scale = ref(1);
const offsetX = ref(0);
const offsetY = ref(0);
const zoomSurface = ref<HTMLElement | null>(null);
const mediaElement = ref<HTMLImageElement | HTMLVideoElement | null>(null);
const pointers = new Map<number, { x: number; y: number }>();
let pinchStartDistance = 0;
let pinchStartScale = 1;
let dragStart: { x: number; y: number; offsetX: number; offsetY: number } | null = null;
let controller: AbortController | null = null;
const mediaTransform = computed(() =>
  props.zoomable
    ? {
        transform: `translate3d(${offsetX.value}px, ${offsetY.value}px, 0) scale(${scale.value})`,
      }
    : undefined,
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
  let waitForImage = false;
  try {
    if (file.previewKind === 'image') {
      if (file.previewRequiresBearer) {
        const blob = await fetchSessionFile(file, 'preview', request.signal);
        if (controller !== request || props.file?.id !== file.id) return;
        objectURL.value = URL.createObjectURL(blob);
        imageURL.value = objectURL.value;
        waitForImage = true;
        return;
      }
      const url = await requestSessionFilePreviewURL(file, request.signal);
      if (controller !== request || props.file?.id !== file.id) return;
      imageURL.value = url;
      waitForImage = true;
      return;
    }
    const blob = await fetchSessionFile(file, 'preview', request.signal);
    if (controller !== request || props.file?.id !== file.id) return;
    if (file.previewKind === 'text') {
      const content = await blob.text();
      if (controller === request && props.file?.id === file.id) text.value = content;
    } else objectURL.value = URL.createObjectURL(blob);
  } catch (err) {
    if (!isAbortError(err) && controller === request) {
      error.value = errorMessage(err, '预览文件失败');
    }
  } finally {
    if (controller === request && !waitForImage) {
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
  imageURL.value = '';
  objectURL.value = '';
  text.value = '';
  error.value = '';
  resetZoom();
}

function finishImageLoad(event: Event) {
  if (event.currentTarget !== mediaElement.value) return;
  controller = null;
  loading.value = false;
}

function failImageLoad(event: Event) {
  if (event.currentTarget !== mediaElement.value) return;
  controller = null;
  loading.value = false;
  error.value = '预览图片失败';
}

function startGesture(event: PointerEvent) {
  if (!props.zoomable || event.pointerType !== 'touch') return;
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (pointers.size === 1) {
    setDragStart(event.clientX, event.clientY);
  } else if (pointers.size === 2) {
    pinchStartDistance = pointerDistance();
    pinchStartScale = scale.value;
    dragStart = null;
  }
}

function moveGesture(event: PointerEvent) {
  if (!props.zoomable || !pointers.has(event.pointerId)) return;
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
  if (pointers.size === 2 && pinchStartDistance > 0) {
    scale.value = Math.min(
      4,
      Math.max(1, pinchStartScale * (pointerDistance() / pinchStartDistance)),
    );
    clampOffset();
    event.preventDefault();
    return;
  }
  if (pointers.size !== 1 || scale.value <= 1 || !dragStart) return;
  offsetX.value = dragStart.offsetX + event.clientX - dragStart.x;
  offsetY.value = dragStart.offsetY + event.clientY - dragStart.y;
  clampOffset();
  event.preventDefault();
}

function endGesture(event: PointerEvent) {
  pointers.delete(event.pointerId);
  pinchStartDistance = 0;
  pinchStartScale = scale.value;
  const remaining = [...pointers.values()][0];
  if (remaining) setDragStart(remaining.x, remaining.y);
  else dragStart = null;
}

function pointerDistance() {
  const [first, second] = [...pointers.values()];
  if (!first || !second) return 0;
  return Math.hypot(second.x - first.x, second.y - first.y);
}

function setDragStart(x: number, y: number) {
  dragStart = { x, y, offsetX: offsetX.value, offsetY: offsetY.value };
}

function clampOffset() {
  const surface = zoomSurface.value;
  const media = mediaElement.value;
  if (!surface || !media || scale.value <= 1) {
    offsetX.value = 0;
    offsetY.value = 0;
    return;
  }
  const maxX = Math.max(0, (media.clientWidth * scale.value - surface.clientWidth) / 2);
  const maxY = Math.max(0, (media.clientHeight * scale.value - surface.clientHeight) / 2);
  offsetX.value = Math.min(maxX, Math.max(-maxX, offsetX.value));
  offsetY.value = Math.min(maxY, Math.max(-maxY, offsetY.value));
}

function resetZoom() {
  pointers.clear();
  pinchStartDistance = 0;
  pinchStartScale = 1;
  dragStart = null;
  scale.value = 1;
  offsetX.value = 0;
  offsetY.value = 0;
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
  position: relative;
  display: grid;
  width: 100%;
  height: 100%;
  place-items: center;
  overflow: clip;
}

.session-file-preview__zoom-surface :deep(.preview-annotator) {
  height: 100%;
}

.session-file-preview__loading {
  position: absolute;
  z-index: 1;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
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

.session-file-preview__model {
  min-height: 68vh;
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
