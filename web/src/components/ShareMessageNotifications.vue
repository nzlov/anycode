<template>
  <Teleport to="body">
    <q-banner
      v-if="messages.length || error"
      class="share-message-notification"
      rounded
      role="status"
      aria-live="polite"
    >
      <template #avatar><q-icon name="mark_chat_unread" color="primary" /></template>
      <div class="text-weight-bold">
        {{ messages.length ? `${messages.length} 条访客消息待审核` : '访客消息提醒' }}
      </div>
      <div v-if="messages[0]" class="share-message-notification__preview">
        {{
          messages[0].content.body || `附件消息（${messages[0].content.attachments.length} 个附件）`
        }}
      </div>
      <div v-if="error" class="text-negative" role="alert">{{ error }}</div>
      <template #action>
        <q-btn v-if="error" flat color="primary" label="重试" :loading="loading" @click="refresh" />
        <q-btn
          v-if="messages.length"
          unelevated
          color="primary"
          label="查看并审核"
          @click="reviewOpen = true"
        />
      </template>
    </q-banner>
  </Teleport>

  <q-dialog v-model="reviewOpen">
    <q-card class="app-content-dialog share-message-inbox">
      <q-card-section class="row no-wrap items-center q-gutter-sm">
        <div class="text-subtitle1 text-weight-bold ellipsis">
          访客消息审核（{{ messages.length }}）
        </div>
        <q-space />
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭访客消息审核" />
      </q-card-section>
      <q-separator />
      <q-card-section class="share-message-inbox__messages">
        <q-banner v-if="error" class="text-negative q-mb-sm" role="alert">{{ error }}</q-banner>
        <ShareMessageReviewCard
          v-for="message in messages"
          :key="message.id"
          :message="message"
          :busy="busy"
          show-session
          @review="review"
          @error="showError"
          @open-session="reviewOpen = false"
        />
      </q-card-section>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue';
import ShareMessageReviewCard from '@/components/ShareMessageReviewCard.vue';
import { useSharingUpdates } from '@/composables/useSharingUpdates';
import {
  getShareMessage,
  listPendingShareMessages,
  reviewShareMessage,
  SharingRequestError,
  type ShareMessage,
  type SharingUpdate,
} from '@/services/sharing';

const messages = ref<ShareMessage[]>([]);
const reviewOpen = ref(false);
const busy = ref('');
const loading = ref(false);
const error = ref('');
let generation = 0;
let revision = 0;
let refreshPromise: Promise<void> | undefined;
let refreshAgain = false;
const messageRequests = new Map<string, symbol>();
const updates = useSharingUpdates(() => null, {
  onData: handleUpdate,
  onError: showError,
  onReset: resetRequests,
});

function showError(e: unknown) {
  error.value = e instanceof Error ? e.message : '读取访客消息失败，请重试';
}

function resetRequests() {
  generation++;
  revision++;
  refreshPromise = undefined;
  refreshAgain = false;
  messageRequests.clear();
  loading.value = false;
  busy.value = '';
}

function refresh(): Promise<void> {
  refreshAgain = true;
  if (refreshPromise) return refreshPromise;
  const current = generation;
  loading.value = true;
  refreshPromise = (async () => {
    while (current === generation && refreshAgain) {
      refreshAgain = false;
      const version = revision;
      try {
        const result = await listPendingShareMessages();
        if (current !== generation) return;
        if (version === revision) messages.value = result;
        else refreshAgain = true;
        error.value = '';
      } catch (e) {
        if (current === generation) showError(e);
      }
    }
  })().finally(() => {
    if (current !== generation) return;
    loading.value = false;
    refreshPromise = undefined;
  });
  return refreshPromise;
}

async function loadMessage(id: string) {
  if (messageRequests.has(id)) return;
  const request = Symbol();
  const current = generation;
  messageRequests.set(id, request);
  try {
    const message = await getShareMessage(id);
    if (current !== generation || messageRequests.get(id) !== request) return;
    messages.value = messages.value.filter((item) => item.id !== id);
    if (message.status === 'pending') {
      messages.value.push(message);
      messages.value.sort(
        (a, b) => a.createdAt.localeCompare(b.createdAt) || a.id.localeCompare(b.id),
      );
    }
    error.value = '';
  } catch (e) {
    if (current === generation && messageRequests.get(id) === request) showError(e);
  } finally {
    if (messageRequests.get(id) === request) messageRequests.delete(id);
  }
}

function handleUpdate(update: SharingUpdate) {
  if (update.eventType === 'sharing.ready') {
    void refresh();
  } else if (update.eventType === 'sharing.message_submitted' && update.messageId) {
    revision++;
    void loadMessage(update.messageId);
  } else if (update.eventType === 'sharing.message_status_changed' && update.messageId) {
    revision++;
    messageRequests.delete(update.messageId);
    if (update.status === 'pending') void loadMessage(update.messageId);
    else messages.value = messages.value.filter((message) => message.id !== update.messageId);
  }
}

async function review(id: string, approved: boolean) {
  if (busy.value) return;
  const current = generation;
  busy.value = id;
  try {
    await reviewShareMessage(id, approved);
    if (current !== generation) return;
    revision++;
    messageRequests.delete(id);
    messages.value = messages.value.filter((message) => message.id !== id);
    error.value = '';
  } catch (e) {
    if (current !== generation) return;
    if (e instanceof SharingRequestError && e.status === 409) await loadMessage(id);
    if (current === generation) showError(e);
  } finally {
    if (current === generation) busy.value = '';
  }
}

watch(
  () => messages.value.length,
  (count) => {
    if (!count) reviewOpen.value = false;
  },
);
onMounted(updates.start);
onUnmounted(updates.stop);
</script>

<style scoped>
.share-message-notification {
  position: fixed;
  z-index: 2500;
  top: calc(12px + env(safe-area-inset-top));
  left: 50%;
  width: min(520px, calc(100vw - 24px));
  transform: translateX(-50%);
  border: 1px solid var(--ac-border);
  background: var(--ac-surface-raised);
  color: var(--ac-text);
  box-shadow: var(--ac-shadow-dialog);
}
.share-message-notification__preview {
  display: -webkit-box;
  overflow: hidden;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
  color: var(--ac-text-muted);
}
.share-message-inbox {
  display: flex;
  flex-direction: column;
  width: min(760px, calc(100vw - 24px)) !important;
  max-height: calc(100dvh - 48px);
  overflow: hidden;
}
.share-message-inbox > :not(.share-message-inbox__messages) {
  flex-shrink: 0;
}
.share-message-inbox__messages {
  flex: 1 1 auto;
  min-height: 0;
  overflow: auto;
}
</style>
