<template>
  <div class="overview-horizontal-session-track">
    <div
      class="overview-horizontal-session"
      :class="`overview-horizontal-session--${sessionLayout}`"
      :style="{ width: `${width}px` }"
    >
      <OverviewHorizontalSessionMobile
        v-if="sessionLayout === 'mobile'"
        :card="card"
      />
      <OverviewHorizontalSessionDesktop v-else :card="card" />
    </div>

    <div
      ref="resizeHandleRef"
      class="overview-horizontal-session-resizer"
      :class="{ 'overview-horizontal-session-resizer--active': resizing }"
      role="separator"
      tabindex="0"
      aria-label="调整会话列宽"
      aria-orientation="vertical"
      :aria-valuemin="minWidth"
      :aria-valuenow="width"
      :aria-valuetext="`会话列 ${width} 像素`"
      @pointerdown="beginResize"
      @pointermove="continueResize"
      @pointerup="endResize"
      @pointercancel="endResize"
      @keydown.left.prevent="resizeBy(-keyboardStep)"
      @keydown.right.prevent="resizeBy(keyboardStep)"
    >
      <span class="overview-horizontal-session-resizer__handle">
        <q-icon name="drag_indicator" size="18px" />
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import OverviewHorizontalSessionDesktop from '@/components/OverviewHorizontalSessionDesktop.vue';
import OverviewHorizontalSessionMobile from '@/components/OverviewHorizontalSessionMobile.vue';
import type { SessionCard } from '@/services/sessions';

const props = defineProps<{
  card: SessionCard;
  width: number;
  minWidth: number;
}>();

const emit = defineEmits<{
  'update:width': [width: number];
}>();

const desktopSessionMinWidth = 1024;
const keyboardStep = 16;
const resizeHandleRef = ref<HTMLElement | null>(null);
const resizing = ref(false);
let resizePointerId = -1;
let resizeStartX = 0;
let resizeStartWidth = 0;

const sessionLayout = computed(() =>
  props.width >= desktopSessionMinWidth ? 'desktop' : 'mobile',
);

function beginResize(event: PointerEvent) {
  if (event.button !== 0) return;
  resizePointerId = event.pointerId;
  resizeStartX = event.clientX;
  resizeStartWidth = props.width;
  resizing.value = true;
  resizeHandleRef.value?.setPointerCapture(event.pointerId);
  event.preventDefault();
}

function continueResize(event: PointerEvent) {
  if (!resizing.value || event.pointerId !== resizePointerId) return;
  setWidth(resizeStartWidth + event.clientX - resizeStartX);
}

function endResize(event: PointerEvent) {
  if (!resizing.value || event.pointerId !== resizePointerId) return;
  if (resizeHandleRef.value?.hasPointerCapture(event.pointerId)) {
    resizeHandleRef.value.releasePointerCapture(event.pointerId);
  }
  resizing.value = false;
  resizePointerId = -1;
}

function resizeBy(delta: number) {
  setWidth(props.width + delta);
}

function setWidth(value: number) {
  if (!Number.isFinite(value)) return;
  emit('update:width', Math.max(props.minWidth, Math.round(value)));
}
</script>

<style scoped>
.overview-horizontal-session-track {
  display: flex;
  height: 100%;
  min-height: 0;
  flex: 0 0 auto;
}

.overview-horizontal-session {
  box-sizing: border-box;
  min-width: 0;
  height: 100%;
  min-height: 0;
  flex: 0 0 auto;
  overflow: hidden;
}

.overview-horizontal-session-resizer {
  position: relative;
  display: flex;
  width: 16px;
  height: 100%;
  flex: 0 0 16px;
  align-items: center;
  justify-content: center;
  outline: 0;
  cursor: col-resize;
  touch-action: none;
}

.overview-horizontal-session-resizer::before {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 1px;
  content: '';
  background: var(--ac-border);
  transform: translateX(-50%);
}

.overview-horizontal-session-resizer__handle {
  position: relative;
  z-index: 1;
  display: flex;
  width: 12px;
  height: 48px;
  align-items: center;
  justify-content: center;
  color: var(--ac-text-muted);
  background: var(--ac-surface-raised);
  border: 1px solid var(--ac-border);
  border-radius: 4px;
}

.overview-horizontal-session-resizer:hover .overview-horizontal-session-resizer__handle,
.overview-horizontal-session-resizer--active .overview-horizontal-session-resizer__handle,
.overview-horizontal-session-resizer:focus-visible .overview-horizontal-session-resizer__handle {
  color: var(--q-primary);
  border-color: var(--q-primary);
  background: var(--ac-surface-muted);
}

.overview-horizontal-session-resizer:focus-visible .overview-horizontal-session-resizer__handle {
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--q-primary) 28%, transparent);
}
</style>
