<template>
  <div class="file-attachment" @click.stop @keydown.stop>
    <q-chip
      v-if="file.previewKind !== 'image'"
      clickable
      square
      outline
      color="primary"
      :removable="removable"
      :disable="disabled"
      :label="label || file.filename"
      icon="attach_file"
      @click="open = true"
      @remove="emit('remove')"
    >
      <q-tooltip>查看 {{ file.filename }}</q-tooltip>
    </q-chip>
    <button
      v-if="file.previewKind === 'image'"
      type="button"
      class="file-attachment__image-trigger"
      :disabled="disabled"
      :aria-label="`放大预览 ${file.filename}`"
      @click="open = true"
    >
      <SessionFilePreview class="file-attachment__inline" :file="file" :annotatable="false" />
      <q-tooltip
        v-if="!open && !$q.platform.has.touch"
        anchor="top middle"
        self="bottom middle"
        :delay="200"
        class="file-attachment__tooltip"
      >
        <SessionFilePreview class="file-attachment__hover" :file="file" :annotatable="false" />
      </q-tooltip>
    </button>
    <q-btn
      v-if="file.previewKind === 'image' && removable"
      round
      dense
      icon="close"
      color="primary"
      class="file-attachment__remove"
      :disable="disabled"
      :aria-label="`移除图片 ${file.filename}`"
      @click="emit('remove')"
    >
      <q-tooltip>移除图片</q-tooltip>
    </q-btn>
    <SessionFilePreview
      v-if="['video', 'audio'].includes(file.previewKind)"
      class="file-attachment__inline"
      :class="{ 'file-attachment__inline--audio': file.previewKind === 'audio' }"
      :file="file"
      :annotatable="false"
    />
    <q-dialog v-model="open" :maximized="$q.screen.lt.md">
      <q-card class="file-attachment__dialog app-content-dialog">
        <q-card-section class="row items-center no-wrap q-gutter-sm">
          <span class="col ellipsis">{{ file.filename }}</span>
          <q-btn
            flat
            round
            dense
            icon="download"
            aria-label="下载文件"
            :loading="downloading"
            @click="download"
          >
            <q-tooltip>下载</q-tooltip>
          </q-btn>
          <q-btn v-close-popup flat round dense icon="close" aria-label="关闭预览" />
        </q-card-section>
        <q-separator />
        <SessionFilePreview :file="file" zoomable :annotatable="false" />
      </q-card>
    </q-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { Notify } from 'quasar';
import SessionFilePreview from '@/components/SessionFilePreview.vue';
import { downloadSessionFile, type SessionFilePreviewData } from '@/services/sessionFiles';

const props = defineProps<{
  file: SessionFilePreviewData;
  label?: string;
  removable?: boolean;
  disabled?: boolean;
}>();
const emit = defineEmits<{ remove: [] }>();
const open = ref(false);
const downloading = ref(false);
async function download() {
  downloading.value = true;
  try {
    await downloadSessionFile(props.file);
  } catch (error) {
    Notify.create({
      type: 'negative',
      message: error instanceof Error ? error.message : '下载失败',
    });
  } finally {
    downloading.value = false;
  }
}
</script>

<style scoped>
.file-attachment {
  position: relative;
  display: inline-flex;
  flex-direction: column;
  min-width: 0;
  max-width: 100%;
}
.file-attachment__remove {
  position: absolute;
  top: 4px;
  right: 4px;
}
.file-attachment :deep(.q-chip__content) {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
}
.file-attachment__inline {
  width: 240px;
  max-width: 100%;
  min-height: 0;
  height: 140px;
  max-height: 140px;
  overflow: auto;
}
.file-attachment__inline--audio {
  height: 64px;
}
.file-attachment__image-trigger {
  display: block;
  padding: 0;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: transparent;
  cursor: zoom-in;
  max-width: 100%;
}
.file-attachment__image-trigger:focus-visible {
  outline: 2px solid var(--q-primary);
  outline-offset: 2px;
}
.file-attachment__tooltip {
  max-width: 90vw;
  padding: 4px;
}
.file-attachment__hover {
  width: min(560px, 80vw);
  height: min(400px, 60vh);
  min-height: 0;
}
.file-attachment__hover :deep(img) {
  max-height: min(400px, 60vh);
}
.file-attachment__inline :deep(img),
.file-attachment__inline :deep(video) {
  max-height: 140px;
  object-fit: contain;
}
.file-attachment__dialog {
  width: 960px;
  max-width: 95vw;
}
.file-attachment__dialog > .session-file-preview {
  max-height: calc(100dvh - 100px);
  overflow: auto;
}
</style>
