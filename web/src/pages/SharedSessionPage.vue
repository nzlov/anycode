<template>
  <q-layout view="hHh lpR fFf">
    <q-header bordered class="bg-primary app-on-primary">
      <q-toolbar>
        <q-icon name="share" size="24px" class="q-mr-sm" />
        <q-toolbar-title>分享会话</q-toolbar-title>
      </q-toolbar>
    </q-header>
    <q-page-container>
      <q-page class="shared-page" :style-fn="pageStyle">
        <q-banner v-if="error" rounded class="text-negative q-mb-md" role="alert">{{
          error
        }}</q-banner>
        <q-card v-if="!sessionId" flat bordered class="claim-card">
          <q-card-section>
            <div class="text-h6">访问分享会话</div>
            <p class="q-mt-sm">领取后可在当前浏览器持续访问此会话。每个链接只能领取一次。</p>
          </q-card-section>
          <q-card-actions>
            <q-btn
              color="primary"
              label="领取并访问"
              :disable="!token || loading"
              :loading="loading"
              @click="claim"
            />
          </q-card-actions>
        </q-card>
        <OverviewHorizontalConversation
          v-else
          class="shared-conversation"
          :session-id="sessionId"
          :share-id="shareId"
          :layout="$q.screen.lt.md ? 'mobile' : 'desktop'"
        />
      </q-page>
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import OverviewHorizontalConversation from '@/components/OverviewHorizontalConversation.vue';
import { claimSessionShare, getSharedSession } from '@/services/sharing';
import { saveShareCredential, shareCredential } from '@/services/sharedAccess';

const route = useRoute();
const router = useRouter();
const shareId = String(route.params.shareId);
const token = ref(String(route.query.token ?? ''));
const sessionId = ref('');
const loading = ref(false);
const error = ref('');
let mounted = true;

function pageStyle(offset: number) {
  const height = `calc(100dvh - ${offset}px)`;
  return { minHeight: height, height };
}

async function claim() {
  loading.value = true;
  error.value = '';
  try {
    const result = await claimSessionShare(shareId, token.value);
    saveShareCredential(shareId, result.credential);
    if (!mounted) return;
    token.value = '';
    await router.replace({ name: 'shared-session', params: { shareId } });
    if (mounted) sessionId.value = result.share.sessionId;
  } catch (e) {
    if (mounted) error.value = e instanceof Error ? e.message : '访问分享会话失败';
  } finally {
    loading.value = false;
  }
}

onMounted(async () => {
  if (!shareCredential(shareId)) return;
  loading.value = true;
  try {
    const share = await getSharedSession(shareId);
    if (mounted) sessionId.value = share.sessionId;
  } catch (e) {
    if (mounted) error.value = e instanceof Error ? e.message : '访问分享会话失败';
  } finally {
    loading.value = false;
  }
});
onUnmounted(() => {
  mounted = false;
});
</script>

<style scoped>
.shared-page {
  display: flex;
  width: 100%;
  max-width: 1600px;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
  margin: auto;
  padding: 12px;
}
.shared-conversation {
  flex: 1 1 0;
  min-height: 0;
}
.claim-card {
  width: 100%;
  max-width: 520px;
  margin: 12vh auto;
}
@media (max-width: 600px) {
  .shared-page {
    padding: 8px;
  }
}
</style>
