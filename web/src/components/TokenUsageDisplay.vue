<template>
  <div class="token-usage-display">
    <span>{{ formatTokenCount(usage.totalTokens) }}</span>
    <q-btn
      flat
      round
      dense
      class="token-usage-display__btn"
      icon="info_outline"
      aria-label="查看 Token 用量明细"
      @pointerenter="openMenu"
      @pointerleave="scheduleMenuClose"
      @click.stop="toggleMenu"
      @contextmenu.stop
      @touchstart.stop
      @keyup.enter.stop
      @keyup.space.stop
    >
      <q-menu
        no-parent-event
        no-focus
        anchor="bottom right"
        self="top right"
        class="token-usage-display__menu"
        :model-value="menuOpen"
        @update:model-value="syncMenuModel"
        @click.stop
        @pointerenter="openMenu"
        @pointerleave="scheduleMenuClose"
      >
        <q-list dense separator class="app-touch-list">
          <q-item>
            <q-item-section>输入 Token</q-item-section>
            <q-item-section side class="token-usage-display__menu-value">
              {{ formatTokenCount(Math.max(usage.inputTokens - usage.cachedInputTokens, 0)) }}
            </q-item-section>
          </q-item>
          <q-item>
            <q-item-section>输出 Token</q-item-section>
            <q-item-section side class="token-usage-display__menu-value">
              {{ formatTokenCount(usage.outputTokens) }}
            </q-item-section>
          </q-item>
          <q-item>
            <q-item-section>缓存 Token</q-item-section>
            <q-item-section side class="token-usage-display__menu-value">
              {{ formatTokenCount(usage.cachedInputTokens) }}
            </q-item-section>
          </q-item>
        </q-list>
      </q-menu>
    </q-btn>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, ref } from 'vue';

import type { TranscriptTokenUsage } from '@/services/sessionTimeline';
import { formatTokenCount } from '@/services/sessionTimelinePresentation';

defineProps<{
  usage: TranscriptTokenUsage;
}>();

const menuHideDelay = 120;
const menuOpen = ref(false);
let menuHideTimer: ReturnType<typeof setTimeout> | null = null;

onBeforeUnmount(clearMenuHideTimer);

function clearMenuHideTimer() {
  if (!menuHideTimer) return;
  clearTimeout(menuHideTimer);
  menuHideTimer = null;
}

function openMenu(event: PointerEvent) {
  if (event.pointerType !== 'mouse') return;
  clearMenuHideTimer();
  menuOpen.value = true;
}

function toggleMenu() {
  clearMenuHideTimer();
  menuOpen.value = !menuOpen.value;
}

function scheduleMenuClose(event: PointerEvent) {
  if (event.pointerType !== 'mouse') return;
  clearMenuHideTimer();
  menuHideTimer = setTimeout(() => {
    menuHideTimer = null;
    menuOpen.value = false;
  }, menuHideDelay);
}

function syncMenuModel(showing: boolean) {
  clearMenuHideTimer();
  menuOpen.value = showing;
}
</script>

<style scoped>
.token-usage-display {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 3px;
  font-variant-numeric: tabular-nums;
}

.token-usage-display > span {
  min-width: 0;
}

.token-usage-display__btn {
  width: 24px;
  min-width: 24px;
  height: 24px;
  min-height: 24px;
  padding: 0;
  color: var(--ac-text-muted);
}

.token-usage-display__btn:hover,
.token-usage-display__btn:focus-visible {
  color: var(--q-primary);
}

.token-usage-display__btn :deep(.q-icon) {
  font-size: 16px;
}

:global(.token-usage-display__menu) {
  min-width: 180px;
}

:global(.token-usage-display__menu-value) {
  color: var(--ac-text);
  font-weight: 700;
}
</style>
