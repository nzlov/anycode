<template>
  <div v-if="images.length" class="event-images">
    <a
      v-for="(image, index) in images"
      :key="`${eventId}-image-${index}`"
      class="event-images__link"
      :href="image.src"
      target="_blank"
      rel="noreferrer"
    >
      <img
        class="event-images__image"
        :src="image.src"
        :alt="`${label} ${index + 1}`"
        loading="lazy"
      />
    </a>
  </div>
</template>

<script setup lang="ts">
import type { TranscriptImage } from '@/services/sessionTimeline';

withDefaults(
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
</script>

<style scoped>
.event-images {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 240px));
  gap: 8px;
  margin-top: 8px;
}

.event-images__link {
  display: block;
  min-width: 0;
}

.event-images__image {
  display: block;
  width: 100%;
  max-height: 320px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  object-fit: contain;
}
</style>
