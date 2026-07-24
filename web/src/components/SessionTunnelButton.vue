<template>
  <q-btn
    v-if="tunnels.length"
    flat
    dense
    no-caps
    class="session-tunnel-btn app-command-btn"
    icon="lan"
    :label="buttonLabel"
    :aria-label="tunnels.length === 1 ? `打开隧道 ${tunnels[0]?.name}` : '选择要打开的隧道'"
    @click.stop="openSingleTunnel"
    @keyup.enter.stop
    @keyup.space.stop
  >
    <q-tooltip v-if="tunnels.length === 1">{{ tunnels[0]?.name }}</q-tooltip>
    <q-menu v-else auto-close @click.stop>
      <q-list dense class="app-touch-list">
        <q-item
          v-for="tunnel in tunnels"
          :key="tunnel.id"
          clickable
          tag="a"
          :href="tunnel.accessUrl"
          target="_blank"
          rel="noopener noreferrer"
        >
          <q-item-section avatar>
            <q-icon name="lan" color="primary" />
          </q-item-section>
          <q-item-section>
            <q-item-label>{{ tunnel.name }}</q-item-label>
            <q-item-label caption>端口 {{ tunnel.port }}</q-item-label>
          </q-item-section>
        </q-item>
      </q-list>
    </q-menu>
  </q-btn>
</template>

<script setup lang="ts">
import { computed } from 'vue';

import type { Tunnel } from '@/services/tunnels';

const props = withDefaults(
  defineProps<{
    tunnels: Tunnel[];
    showCount?: boolean;
  }>(),
  { showCount: false },
);
const buttonLabel = computed(() => {
  if (props.tunnels.length === 1) return props.tunnels[0]?.name;
  return props.showCount ? String(props.tunnels.length) : undefined;
});

function openSingleTunnel() {
  if (props.tunnels.length !== 1) return;
  const tunnel = props.tunnels[0];
  if (tunnel) window.open(tunnel.accessUrl, '_blank', 'noopener,noreferrer');
}
</script>

<style scoped>
.session-tunnel-btn {
  max-width: 180px;
}

.session-tunnel-btn :deep(.q-btn__content),
.session-tunnel-btn :deep(.block) {
  min-width: 0;
  flex-wrap: nowrap;
}

.session-tunnel-btn :deep(.block) {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
