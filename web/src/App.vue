<template>
  <router-view />
  <a
    v-if="dailyBackgroundState"
    class="app-daily-credit"
    :href="dailyBackgroundState.sourceUrl"
    target="_blank"
    rel="noopener noreferrer"
    :aria-label="`查看图片来源：${dailyBackgroundState.title}，${dailyBackgroundState.copyright}`"
  >
    <q-icon name="photo_camera" size="18px" />
    <span class="app-daily-credit__text">
      <span class="app-daily-credit__title">{{ dailyBackgroundState.title }}</span>
      <span class="app-daily-credit__copyright">{{ dailyBackgroundState.copyright }}</span>
    </span>
    <q-tooltip>{{ dailyBackgroundState.title }} · {{ dailyBackgroundState.copyright }}</q-tooltip>
  </a>
</template>

<script setup lang="ts">
import { dailyBackgroundState } from '@/theme/dailyBackground';
</script>
