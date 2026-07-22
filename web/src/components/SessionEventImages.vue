<template>
  <div v-if="images.length" class="event-images">
    <a
      v-for="(image, index) in images"
      :key="`${eventId}-image-${index}`"
      class="event-images__link"
      :href="image.src"
      target="_blank"
      rel="noreferrer"
      @click="openImage($event, image, index)"
    >
      {{ label }} {{ index + 1 }}
    </a>
  </div>
</template>

<script setup lang="ts">
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

function openImage(event: MouseEvent, image: TranscriptImage, index: number) {
  if (!resourceOpener?.(image.src, `${props.label} ${index + 1}`)) return;
  event.preventDefault();
  event.stopPropagation();
}
</script>

<style scoped>
.event-images {
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
