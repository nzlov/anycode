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
        :priority-loading="priorityLoading"
        @set-priority="emit('set-priority', $event)"
      />
      <OverviewHorizontalSessionDesktop
        v-else
        :card="card"
        :priority-loading="priorityLoading"
        @set-priority="emit('set-priority', $event)"
      />
      <SessionCardContextMenu
        :card="card"
        :priority-loading="priorityLoading"
        :close-loading="closeLoading"
        @set-priority="emit('set-priority', $event)"
        @close="emit('close')"
      />
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
    ></div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import OverviewHorizontalSessionDesktop from '@/components/OverviewHorizontalSessionDesktop.vue';
import OverviewHorizontalSessionMobile from '@/components/OverviewHorizontalSessionMobile.vue';
import SessionCardContextMenu from '@/components/SessionCardContextMenu.vue';
import type { SessionCard, SessionPriority } from '@/services/sessions';

const props = defineProps<{
  card: SessionCard;
  width: number;
  minWidth: number;
  priorityLoading?: boolean;
  closeLoading?: boolean;
}>();

const emit = defineEmits<{
  'update:width': [width: number];
  'set-priority': [priority: SessionPriority];
  close: [];
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
  position: relative;
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
  position: absolute;
  z-index: 2;
  top: 0;
  right: -24px;
  width: 24px;
  height: 100%;
  outline: 0;
  cursor: col-resize;
  touch-action: none;
}

.overview-horizontal-session-resizer::before {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 50%;
  width: 2px;
  content: '';
  background: transparent;
  transform: translateX(-50%);
}

.overview-horizontal-session-track:last-child .overview-horizontal-session-resizer {
  display: none;
}

.overview-horizontal-session-resizer:hover::before,
.overview-horizontal-session-resizer--active::before,
.overview-horizontal-session-resizer:focus-visible::before {
  background: var(--q-primary);
}
</style>
