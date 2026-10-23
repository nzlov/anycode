<template>
  <div v-if="images.length" class="event-images">
    <template v-for="(image, index) in images" :key="`${eventId}-image-${index}`">
      <SessionFileAttachment
        v-if="storedImage(image.src)"
        :file="storedImage(image.src)!"
        :label="`${label} ${index + 1}`"
      />
      <a
        v-else
        class="event-images__link"
        :href="image.src"
        target="_blank"
        rel="noreferrer"
        @click="openImage($event, image, index)"
      >
        {{ label }} {{ index + 1 }}
      </a>
    </template>
  </div>
</template>

<script setup lang="ts">
import SessionFileAttachment from '@/components/SessionFileAttachment.vue';
import type { SessionFilePreviewData } from '@/services/sessionFiles';
import type { TranscriptImage } from '@/services/sessionTimeline';
import { useSessionEventResourceOpener } from '@/services/sessionEventResources';

const props = withDefaults(
  defineProps<{
    eventId: string;
    images?: TranscriptImage[];
    label?: string;
  }>(),
  {
    images: () => [],
    label: '事件图片',
  },
);
const resourceOpener = useSessionEventResourceOpener();

function storedImage(src: string): SessionFilePreviewData | null {
  const match = src.match(/^\/files\/([^/]+)\/preview(?:[?#].*)?$/);
  if (!match?.[1]) return null;
  return {
    id: decodeURIComponent(match[1]),
    filename: props.label,
    size: 0,
    previewKind: 'image',
    previewUrl: src,
    downloadUrl: `/files/${match[1]}/download`,
  };
}

function openImage(event: MouseEvent, image: TranscriptImage, index: number) {
  if (!resourceOpener?.(image.src, `${props.label} ${index + 1}`)) return;
  event.preventDefault();
  event.stopPropagation();
}
</script>

<style scoped>
.event-images {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 4px;
}

.event-images__link {
  display: inline;
  min-width: 0;
  margin-right: 12px;
  color: var(--q-primary);
  cursor: pointer;
  overflow-wrap: anywhere;
}

.event-images__link:last-child {
  margin-right: 0;
}
</style>
