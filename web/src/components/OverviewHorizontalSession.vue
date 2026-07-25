<template>
  <div class="overview-horizontal-session-track">
    <div
      class="overview-horizontal-session"
      :class="`overview-horizontal-session--${sessionLayout}`"
      :style="{ width: `${displayWidth}px` }"
    >
      <OverviewHorizontalSessionDesktop
        v-if="card.mode === 'terminal' || sessionLayout === 'desktop'"
        :card="card"
        :tunnels="tunnels"
        :priority-loading="priorityLoading"
        :close-loading="closeLoading"
        :terminal-resize-paused="resizing"
        @set-priority="emit('set-priority', $event)"
        @terminal-opened="emit('terminal-opened', $event)"
        @close="emit('close')"
      />
      <OverviewHorizontalSessionMobile
        v-else
        :card="card"
        :tunnels="tunnels"
        :priority-loading="priorityLoading"
        :close-loading="closeLoading"
        @set-priority="emit('set-priority', $event)"
        @terminal-opened="emit('terminal-opened', $event)"
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
      :aria-valuenow="displayWidth"
      :aria-valuetext="`会话列 ${displayWidth} 像素`"
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
import { computed, ref, watch } from 'vue';

import OverviewHorizontalSessionDesktop from '@/components/OverviewHorizontalSessionDesktop.vue';
import OverviewHorizontalSessionMobile from '@/components/OverviewHorizontalSessionMobile.vue';
import type { SessionCard, SessionPriority } from '@/services/sessions';
import type { Tunnel } from '@/services/tunnels';

const props = defineProps<{
  card: SessionCard;
  tunnels: Tunnel[];
  width: number;
  minWidth: number;
  priorityLoading?: boolean;
  closeLoading?: boolean;
}>();

const emit = defineEmits<{
  'update:width': [width: number];
  'set-priority': [priority: SessionPriority];
  'terminal-opened': [sessionId: string];
  close: [];
}>();

const desktopSessionMinWidth = 1024;
const keyboardStep = 16;
const resizeHandleRef = ref<HTMLElement | null>(null);
const resizing = ref(false);
const displayWidth = ref(props.width);
let resizePointerId = -1;
let resizeStartX = 0;
let resizeStartWidth = 0;

const sessionLayout = computed(() =>
  displayWidth.value >= desktopSessionMinWidth ? 'desktop' : 'mobile',
);

watch(
  () => props.width,
  (width) => {
    if (!resizing.value) displayWidth.value = width;
  },
);

function beginResize(event: PointerEvent) {
  if (event.button !== 0) return;
  resizePointerId = event.pointerId;
  resizeStartX = event.clientX;
  resizeStartWidth = displayWidth.value;
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
  emit('update:width', displayWidth.value);
}

function resizeBy(delta: number) {
  setWidth(props.width + delta);
}

function setWidth(value: number) {
  if (!Number.isFinite(value)) return;
  const width = Math.max(props.minWidth, Math.round(value));
  displayWidth.value = width;
  if (!resizing.value) emit('update:width', width);
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
  width: 12px;
  height: 100%;
  flex: 0 0 12px;
  outline: 0;
  background: transparent;
  cursor: col-resize;
  touch-action: none;
}

.overview-horizontal-session-resizer:hover,
.overview-horizontal-session-resizer--active,
.overview-horizontal-session-resizer:focus-visible {
  background: var(--q-primary);
}
</style>
