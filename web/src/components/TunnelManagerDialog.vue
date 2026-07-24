<template>
  <component
    :is="page ? 'div' : QDialog"
    :model-value="page ? undefined : modelValue"
    @update:model-value="page ? undefined : emit('update:modelValue', $event)"
  >
    <q-card class="tunnel-manager app-content-dialog">
      <q-card-section class="tunnel-manager__header row items-center">
        <div class="text-subtitle1 text-weight-bold">隧道</div>
        <q-space />
        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="refresh"
          aria-label="刷新隧道"
          :loading="loading"
          @click="refresh"
        >
          <q-tooltip>刷新</q-tooltip>
        </q-btn>
        <q-btn flat round dense class="app-icon-btn" icon="close" aria-label="关闭" @click="close">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />
      <q-linear-progress v-if="loading" indeterminate color="primary" />

      <q-card-section v-if="error" class="q-pb-none">
        <q-banner dense rounded class="tunnel-manager__error">
          {{ error }}
          <template #action>
            <q-btn flat dense label="重试" no-caps @click="refresh" />
          </template>
        </q-banner>
      </q-card-section>

      <q-card-section class="tunnel-manager__content">
        <q-list v-if="tunnels.length" bordered separator class="tunnel-list">
          <q-item v-for="tunnel in tunnels" :key="tunnel.id" class="tunnel-list__item">
            <q-item-section avatar top>
              <q-icon name="lan" color="primary" size="24px" />
            </q-item-section>
            <q-item-section class="tunnel-list__main">
              <q-item-label class="row items-center q-gutter-sm">
                <span class="text-weight-medium">端口 {{ tunnel.port }}</span>
                <q-badge outline color="positive" :label="statusLabel(tunnel.status)" />
              </q-item-label>
              <q-item-label caption class="tunnel-list__url">
                <a :href="tunnel.accessUrl" target="_blank" rel="noopener noreferrer">
                  {{ tunnel.accessUrl }}
                </a>
              </q-item-label>
              <q-item-label caption lines="1">会话 {{ tunnel.sessionId }}</q-item-label>
              <q-item-label caption>{{ formatCreatedAt(tunnel.createdAt) }}</q-item-label>
            </q-item-section>
            <q-item-section side top>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                color="negative"
                icon="close"
                :aria-label="`关闭端口 ${tunnel.port} 的隧道`"
                :loading="closingId === tunnel.id"
                :disable="Boolean(closingId)"
                @click="confirmClose(tunnel)"
              >
                <q-tooltip>关闭隧道</q-tooltip>
              </q-btn>
            </q-item-section>
          </q-item>
        </q-list>

        <div v-else-if="!loading && !error" class="tunnel-manager__empty">
          <q-icon name="lan" size="40px" />
          <div>暂无运行中的隧道</div>
        </div>
      </q-card-section>

      <q-dialog v-model="closeDialogOpen">
        <q-card class="confirm-dialog">
          <q-card-section class="row items-center q-pb-sm">
            <div class="text-subtitle1 text-weight-bold">关闭隧道</div>
            <q-space />
            <q-btn
              v-close-popup
              flat
              round
              dense
              class="app-icon-btn"
              icon="close"
              aria-label="取消"
            />
          </q-card-section>
          <q-separator />
          <q-card-section>
            确认关闭端口 {{ selectedTunnel?.port }} 的隧道？外部地址将立即失效。
          </q-card-section>
          <q-card-actions align="right">
            <q-btn v-close-popup flat label="取消" color="primary" no-caps />
            <q-btn
              unelevated
              label="关闭"
              color="negative"
              no-caps
              :loading="Boolean(closingId)"
              @click="closeSelectedTunnel"
            />
          </q-card-actions>
        </q-card>
      </q-dialog>
    </q-card>
  </component>
</template>

<script setup lang="ts">
import { QDialog } from 'quasar';
import { ref, watch } from 'vue';

import { closeTunnel as closeTunnelRequest, listTunnels, type Tunnel } from '@/services/tunnels';

const props = defineProps<{
  modelValue: boolean;
  page?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const tunnels = ref<Tunnel[]>([]);
const loading = ref(false);
const error = ref('');
const closingId = ref('');
const closeDialogOpen = ref(false);
const selectedTunnel = ref<Tunnel | null>(null);

watch(
  () => props.page || props.modelValue,
  (active) => {
    if (active) void refresh();
  },
  { immediate: true },
);

async function refresh() {
  loading.value = true;
  error.value = '';
  try {
    tunnels.value = await listTunnels();
  } catch (err) {
    error.value = err instanceof Error ? err.message : '加载隧道失败';
  } finally {
    loading.value = false;
  }
}

function close() {
  emit('update:modelValue', false);
}

function confirmClose(tunnel: Tunnel) {
  selectedTunnel.value = tunnel;
  closeDialogOpen.value = true;
}

async function closeSelectedTunnel() {
  const tunnel = selectedTunnel.value;
  if (!tunnel || closingId.value) return;
  closingId.value = tunnel.id;
  error.value = '';
  try {
    await closeTunnelRequest(tunnel.id);
    tunnels.value = tunnels.value.filter((item) => item.id !== tunnel.id);
    closeDialogOpen.value = false;
    selectedTunnel.value = null;
  } catch (err) {
    error.value = err instanceof Error ? err.message : '关闭隧道失败';
    closeDialogOpen.value = false;
  } finally {
    closingId.value = '';
  }
}

function statusLabel(status: string) {
  return status === 'running' ? '运行中' : status;
}

function formatCreatedAt(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date);
}
</script>

<style scoped>
.tunnel-manager {
  width: min(680px, calc(100vw - 32px));
  max-width: 680px;
}

.tunnel-manager__header {
  min-height: 56px;
}

.tunnel-manager__content {
  min-height: 180px;
  max-height: min(68vh, 640px);
  overflow: auto;
}

.tunnel-manager__error {
  color: var(--ac-status-danger-text);
  background: var(--ac-status-danger-bg);
}

.tunnel-list__item {
  align-items: flex-start;
}

.tunnel-list__main {
  min-width: 0;
}

.tunnel-list__url {
  overflow-wrap: anywhere;
}

.tunnel-list__url a {
  color: var(--q-primary);
}

.tunnel-manager__empty {
  min-height: 140px;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 8px;
  color: var(--app-text-muted);
}

@media (max-width: 599px) {
  .tunnel-manager {
    width: 100%;
    max-width: none;
    min-height: calc(100dvh - 50px);
    border-radius: 0;
  }

  .tunnel-manager__content {
    max-height: none;
  }
}
</style>
