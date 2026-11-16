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
          @click="refresh()"
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
          <ShareMessageReviewCard
            v-for="message in messages"
            :key="message.id"
            :message="message"
            :busy="busy"
            @review="review"
            @error="showError"
          />
        </q-tab-panel>
      </q-tab-panels>
    </q-card>
  </q-dialog>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { copyToClipboard, Notify } from 'quasar';
import { useSharingUpdates } from '@/composables/useSharingUpdates';
import ShareMessageReviewCard from '@/components/ShareMessageReviewCard.vue';
import {
  createSessionShare,
  listSessionShares,
  listShareMessages,
  revokeSessionShare,
  reviewShareMessage,
  savedShareURL,
  type SessionShare,
  type ShareMessage,
  type SharingUpdate,
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
let generation = 0;
let stopped = true;
let refreshPromise: Promise<void> | undefined;
let reloadLinks = false;
let reloadMessages = false;
let linksVersion = 0;
let messagesVersion = 0;
const updates = useSharingUpdates(() => props.sessionId, {
  onData: handleUpdate,
  onError: showError,
  onReset: resetRequests,
});
function showError(e: unknown) {
  error.value = e instanceof Error ? e.message : '操作失败';
}
function refresh(links = open.value, requests = true): Promise<void> {
  if (stopped) return Promise.resolve();
  reloadLinks ||= links;
  reloadMessages ||= requests;
  if (refreshPromise) return refreshPromise;
  const request = generation;
  loading.value = true;
  refreshPromise = (async () => {
    while (request === generation && (reloadLinks || reloadMessages)) {
      const fetchLinks = reloadLinks;
      const fetchMessages = reloadMessages;
      const linkVersion = linksVersion;
      const messageVersion = messagesVersion;
      reloadLinks = reloadMessages = false;
      try {
        const [links, requests] = await Promise.all([
          fetchLinks ? listSessionShares(props.sessionId) : null,
          fetchMessages ? listShareMessages(props.sessionId) : null,
        ]);
        if (request !== generation) return;
        if (links && linkVersion === linksVersion) shares.value = links;
        if (requests && messageVersion === messagesVersion) messages.value = requests;
        reloadLinks ||= !!links && linkVersion !== linksVersion;
        reloadMessages ||= !!requests && messageVersion !== messagesVersion;
        error.value = '';
      } catch (e) {
        if (request === generation) showError(e);
      }
    }
  })().finally(() => {
    if (request !== generation) return;
    loading.value = false;
    refreshPromise = undefined;
  });
  return refreshPromise;
}
function handleUpdate(update: SharingUpdate) {
  if (update.sessionId !== props.sessionId) return;
  switch (update.eventType) {
    case 'sharing.ready':
      void refresh();
      break;
    case 'sharing.link_created':
    case 'sharing.link_claimed':
    case 'sharing.link_revoked': {
      linksVersion++;
      const share = shares.value.find((item) => item.id === update.shareId);
      if (share && update.eventType === 'sharing.link_claimed') {
        share.claimedAt = update.occurredAt;
      } else if (share && update.eventType === 'sharing.link_revoked') {
        share.revokedAt = update.occurredAt;
      } else if (open.value) {
        void refresh(true, false);
      }
      break;
    }
    case 'sharing.message_submitted':
      messagesVersion++;
      void refresh(false);
      break;
    case 'sharing.message_status_changed': {
      messagesVersion++;
      const message = messages.value.find((item) => item.id === update.messageId);
      if (message && update.status) message.status = update.status;
      else void refresh(false);
      break;
    }
  }
}
function start() {
  if (!stopped) return;
  stopped = false;
  updates.start();
}
function stop() {
  stopped = true;
  updates.stop();
}
function resetRequests() {
  generation++;
  refreshPromise = undefined;
  reloadLinks = reloadMessages = false;
  loading.value = false;
}
async function create() {
  creating.value = true;
  try {
    const link = await createSessionShare(props.sessionId);
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
  }
}
watch(open, (value) => {
  if (value) void refresh();
});
watch(
  () => props.sessionId,
  () => {
    stop();
    shares.value = [];
    messages.value = [];
    error.value = '';
    start();
  },
);
onMounted(start);
onUnmounted(stop);
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
.share-main {
  min-width: 0;
}
.share-id,
.share-url {
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
