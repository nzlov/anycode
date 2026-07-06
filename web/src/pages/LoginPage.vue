<template>
  <div class="login-page">
    <q-card flat bordered class="login-panel">
      <q-card-section class="login-panel__header">
        <div class="text-h6 text-weight-bold">AnyCode</div>
        <div class="text-body2 text-muted">输入访问密钥后进入工作台</div>
      </q-card-section>

      <q-card-section>
        <q-input
          v-model="accessKey"
          outlined
          autofocus
          label="访问密钥"
          :type="visible ? 'text' : 'password'"
          @keyup.enter="login"
        >
          <template #prepend>
            <q-icon name="key" />
          </template>
          <template #append>
            <q-btn
              flat
              round
              dense
              :icon="visible ? 'visibility_off' : 'visibility'"
              aria-label="切换密钥可见性"
              @click="visible = !visible"
            >
              <q-tooltip>{{ visible ? '隐藏密钥' : '显示密钥' }}</q-tooltip>
            </q-btn>
          </template>
        </q-input>
      </q-card-section>

      <q-card-actions align="right">
        <q-btn unelevated color="primary" icon="login" label="进入" no-caps :loading="loading" @click="login" />
      </q-card-actions>
    </q-card>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import { setGraphQLAccessKey, verifyGraphQLAccessKey } from '@/services/graphqlClient';

const $q = useQuasar();
const route = useRoute();
const router = useRouter();
const accessKey = ref('');
const visible = ref(false);
const loading = ref(false);

async function login() {
  const key = accessKey.value.trim();
  if (!key) {
    notifyError('请输入访问密钥');
    return;
  }
  loading.value = true;
  try {
    const ok = await verifyGraphQLAccessKey(key);
    if (!ok) {
      notifyError('访问密钥无效');
      return;
    }
    setGraphQLAccessKey(key);
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/';
    await router.replace(redirect);
  } catch {
    notifyError('无法连接服务');
  } finally {
    loading.value = false;
  }
}

function notifyError(message: string) {
  $q.notify({
    type: 'negative',
    icon: 'error',
    position: 'top-right',
    message,
    timeout: 5000,
    actions: [{ icon: 'close', color: 'white', round: true }],
  });
}
</script>

<style scoped>
.login-page {
  display: grid;
  min-height: 100vh;
  place-items: center;
  padding: 24px;
  background: var(--ac-bg);
}

.login-panel {
  width: min(420px, 100%);
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.login-panel__header {
  display: grid;
  gap: 4px;
  padding-bottom: 8px;
}
</style>
