<template>
  <q-dialog v-model="open">
    <q-card class="app-content-dialog share-manager">
      <q-card-section class="row no-wrap items-center q-gutter-sm">
        <div class="text-subtitle1 text-weight-bold ellipsis">会话分享与审核</div>
        <q-space />
        <q-btn
          flat
          round
          dense
          icon="refresh"
          aria-label="刷新分享"
          :loading="loading"
          @click="refresh"
          ><q-tooltip>刷新</q-tooltip></q-btn
        >
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭分享管理" />
      </q-card-section>
      <q-separator />
      <q-card-section>
        <q-btn
          color="primary"
          unelevated
          icon="add_link"
          label="生成一次性分享链接"
          :loading="creating"
          @click="create"
        />
        <div class="text-caption text-muted q-mt-sm">
          每个链接仅可领取一次，可同时分享给多人。每条消息单独审核，通过后按批准顺序发送。
        </div>
        <q-banner v-if="error" class="text-negative q-mt-sm" role="alert">{{ error }}</q-banner>
      </q-card-section>
      <q-tabs v-model="tab" dense align="left"
        ><q-tab name="links" label="已分享链接" /><q-tab
          name="review"
          :label="`消息审核${pending ? ` (${pending})` : ''}`"
      /></q-tabs>
      <q-separator />
      <q-tab-panels v-model="tab" class="share-panels">
        <q-tab-panel name="links">
          <q-list v-if="shares.length" bordered separator>
            <q-item v-for="share in shares" :key="share.id" class="share-row">
              <q-item-section avatar top><q-icon name="link" color="primary" /></q-item-section>
              <q-item-section class="share-main">
                <q-item-label class="share-id">{{ share.id }}</q-item-label>
                <q-item-label>
                  <q-badge
                    outline
                    :color="share.revokedAt ? 'grey' : share.claimedAt ? 'positive' : 'primary'"
                    :label="share.revokedAt ? '已撤销' : share.claimedAt ? '已领取' : '待领取'"
                /></q-item-label>
                <q-item-label caption>{{
                  new Date(share.createdAt).toLocaleString()
                }}</q-item-label>
                <q-item-label
                  v-if="!share.claimedAt && !share.revokedAt"
                  caption
                  class="share-url"
                  >{{
                    savedShareURL(share.id) || '完整链接仅在创建浏览器保留，可重新生成链接'
                  }}</q-item-label
                >
              </q-item-section>
              <q-item-section side top>
                <div class="row no-wrap">
                  <q-btn
                    v-if="!share.claimedAt && !share.revokedAt && savedShareURL(share.id)"
                    flat
                    round
                    dense
                    icon="content_copy"
                    aria-label="复制分享链接"
                    @click="copy(share.id)"
                    ><q-tooltip>复制链接</q-tooltip></q-btn
                  >
                  <q-btn
                    v-if="!share.revokedAt"
                    flat
                    round
                    dense
                    color="negative"
                    icon="link_off"
                    aria-label="撤销分享"
                    :disable="!!busy"
                    @click="revoke(share.id)"
                    ><q-tooltip>撤销分享</q-tooltip></q-btn
                  >
                </div>
              </q-item-section>
            </q-item>
          </q-list>
          <div v-else class="text-muted q-pa-lg text-center">暂无分享链接</div>
        </q-tab-panel>
        <q-tab-panel name="review">
          <div v-if="!messages.length" class="text-muted q-pa-lg text-center">暂无访客消息</div>
          <q-card v-for="message in messages" :key="message.id" flat bordered class="q-mb-md">
            <q-card-section>
              <div class="share-message-heading">
                <q-badge :color="message.status === 'pending' ? 'warning' : 'grey'">{{
                  shareMessageStatus(message.status)
                }}</q-badge
                ><span class="text-caption share-id">分享 {{ message.shareId }}</span>
              </div>
              <div class="text-caption text-muted q-mt-sm">
                {{ new Date(message.createdAt).toLocaleString() }} ·
                {{ message.content.config.CodexModel || '默认模型' }} ·
                {{ message.content.config.ReasoningEffort || '默认强度' }} ·
                {{ message.content.config.PermissionMode
                }}{{ message.content.config.FastMode ? ' · Fast' : '' }}
              </div>
              <div class="share-body q-mt-sm">{{ message.content.body }}</div>
              <q-btn
                v-for="file in message.content.attachments"
                :key="file.id"
                flat
                dense
                icon="attach_file"
                :label="file.filename"
                @click="downloadReviewAttachment(message.id, file).catch(showError)"
              />
            </q-card-section>
            <q-card-actions v-if="message.status === 'pending'" align="right">
              <q-btn
                flat
                color="negative"
                label="拒绝"
                :disable="!!busy"
                @click="review(message.id, false)"
              />
              <q-btn
                unelevated
                color="primary"
                label="批准并发送"
                :loading="busy === message.id"
                :disable="!!busy"
                @click="review(message.id, true)"
              />
            </q-card-actions>
          </q-card>
        </q-tab-panel>
      </q-tab-panels>
    </q-card>
  </q-dialog>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { copyToClipboard, Notify } from 'quasar';
import {
  createSessionShare,
  listSessionShares,
  listShareMessages,
  revokeSessionShare,
  reviewShareMessage,
  savedShareURL,
  downloadReviewAttachment,
  shareMessageStatus,
  type SessionShare,
  type ShareMessage,
} from '@/services/sharing';
const props = defineProps<{ sessionId: string }>();
const emit = defineEmits<{ pending: [count: number] }>();
const open = defineModel<boolean>({ default: false });
const shares = ref<SessionShare[]>([]);
const messages = ref<ShareMessage[]>([]);
const loading = ref(false);
const creating = ref(false);
const busy = ref('');
const error = ref('');
const tab = ref('links');
const pending = computed(() => messages.value.filter((m) => m.status === 'pending').length);
watch(pending, (value) => emit('pending', value));
let timer: ReturnType<typeof setInterval> | undefined;
let generation = 0;
function showError(e: unknown) {
  error.value = e instanceof Error ? e.message : '操作失败';
}
async function refresh() {
  if (loading.value) return;
  const request = generation;
  loading.value = true;
  try {
    const [links, requests] = await Promise.all([
      listSessionShares(props.sessionId),
      listShareMessages(props.sessionId),
    ]);
    if (request !== generation) return;
    shares.value = links;
    messages.value = requests;
    error.value = '';
  } catch (e) {
    if (request === generation) showError(e);
  } finally {
    loading.value = false;
  }
}
async function create() {
  creating.value = true;
  try {
    const link = await createSessionShare(props.sessionId);
    await refresh();
    try {
      await copyToClipboard(link.url);
      Notify.create({ type: 'positive', message: '分享链接已复制' });
    } catch {
      Notify.create({ type: 'info', message: '链接已生成，可在列表中选择并复制' });
    }
  } catch (e) {
    showError(e);
  } finally {
    creating.value = false;
  }
}
async function copy(id: string) {
  try {
    await copyToClipboard(savedShareURL(id));
    Notify.create({ type: 'positive', message: '分享链接已复制' });
  } catch {
    Notify.create({ type: 'info', message: '请在列表中选择链接并手动复制' });
  }
}
async function revoke(id: string) {
  busy.value = id;
  try {
    await revokeSessionShare(id);
    await refresh();
  } catch (e) {
    showError(e);
  } finally {
    busy.value = '';
  }
}
async function review(id: string, approved: boolean) {
  busy.value = id;
  try {
    await reviewShareMessage(id, approved);
  } catch (e) {
    showError(e);
  } finally {
    busy.value = '';
    await refresh();
  }
}
watch(open, (value) => {
  if (value) void refresh();
});
onMounted(() => {
  void refresh();
  timer = setInterval(() => void refresh(), 3000);
});
onUnmounted(() => {
  generation++;
  if (timer) clearInterval(timer);
});
</script>
<style scoped>
.app-content-dialog.share-manager {
  display: flex;
  flex-direction: column;
  width: min(760px, calc(100vw - 24px)) !important;
  max-height: min(90dvh, calc(100dvh - 48px)) !important;
  overflow: hidden;
}
.share-manager > :not(.share-panels) {
  flex-shrink: 0;
}
.share-panels {
  flex: 1 1 auto;
  min-height: 0;
  min-width: 0;
  width: 100%;
  overflow: auto;
}
.share-panels :deep(.q-tab-panel) {
  width: 100%;
  min-width: 0;
}
.share-message-heading {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.share-main {
  min-width: 0;
}
.share-id,
.share-url,
.share-body {
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
@media (max-width: 480px) {
  .share-panels :deep(.q-tab-panel) {
    padding: 12px;
  }
  .share-row {
    padding: 12px 8px;
  }
  .share-row :deep(.q-item__section--avatar) {
    min-width: 28px;
  }
}
</style>
