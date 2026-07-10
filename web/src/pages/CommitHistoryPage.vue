<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">提交记录</div>
        <div class="text-body2 text-muted">
          {{ session?.title ?? sessionId }} · {{ session?.branch ?? '-' }}
        </div>
      </div>
      <q-btn
        flat
        round
        dense
        icon="arrow_back"
        aria-label="返回卡片"
        :to="`/sessions/${sessionId}`"
      >
        <q-tooltip>返回卡片</q-tooltip>
      </q-btn>
    </div>

    <q-card flat bordered class="commit-history-card">
      <q-card-section v-if="loading" class="state-content">
        <q-spinner color="primary" size="28px" />
        <div class="text-body2 text-muted">正在加载提交记录</div>
      </q-card-section>
      <q-card-section v-else-if="!history?.available" class="commit-history-empty">
        <q-icon name="block" color="grey-6" size="32px" />
        <div>
          <div class="text-subtitle1 text-weight-bold">提交记录不可用</div>
          <div class="text-body2 text-muted">
            当前卡片没有可读取的 git worktree 或项目不是 git 仓库。
          </div>
        </div>
      </q-card-section>
      <q-card-section v-else-if="history.commits.length === 0" class="commit-history-empty">
        <q-icon name="commit" color="primary" size="32px" />
        <div>
          <div class="text-subtitle1 text-weight-bold">暂无切出后的提交</div>
          <div class="text-body2 text-muted">当前卡片分支相对基础分支还没有新增提交。</div>
        </div>
      </q-card-section>
      <q-list v-else separator class="commit-history-list">
        <q-item v-for="commit in history.commits" :key="commit.hash">
          <q-item-section avatar>
            <q-icon name="commit" color="primary" />
          </q-item-section>
          <q-item-section>
            <q-item-label class="commit-subject">{{
              commit.subject || commit.shortHash
            }}</q-item-label>
            <q-item-label caption>
              <span class="commit-hash">{{ commit.shortHash }}</span>
              <span class="q-mx-xs">·</span>
              <span>{{ commit.authorName }}</span>
              <span class="q-mx-xs">·</span>
              <span>{{ formatTime(commit.createdAt) }}</span>
            </q-item-label>
          </q-item-section>
        </q-item>
      </q-list>
    </q-card>

    <AppPagination
      v-if="pageMax > 1"
      v-model="page"
      class="justify-center q-mt-md"
      :max="pageMax"
      :disabled="loading"
    />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { Notify } from 'quasar';
import { useRoute } from 'vue-router';

import AppPagination from '@/components/AppPagination.vue';
import { getSessionCommitHistory, type SessionCommitHistory } from '@/services/diff';
import { getSession, type SessionDetail } from '@/services/sessions';

const route = useRoute();
const sessionId = String(route.params.id ?? '');
const session = ref<SessionDetail | null>(null);
const history = ref<SessionCommitHistory | null>(null);
const loading = ref(false);
const page = ref(1);
const pageSize = 20;
const pageMax = computed(() => {
  const info = history.value?.pageInfo;
  if (!info || info.total < 1) return 1;
  return Math.max(1, Math.ceil(info.total / info.pageSize));
});

async function loadPage() {
  loading.value = true;
  try {
    const [nextSession, nextHistory] = await Promise.all([
      session.value ? Promise.resolve(session.value) : getSession(sessionId),
      getSessionCommitHistory({ sessionId, page: page.value, pageSize }),
    ]);
    session.value = nextSession;
    history.value = nextHistory;
    page.value = nextHistory.pageInfo.page;
  } catch (err) {
    if (!wasNotified(err)) {
      Notify.create({
        type: 'negative',
        icon: 'error',
        position: 'top-right',
        message: err instanceof Error ? err.message || '加载提交记录失败' : '加载提交记录失败',
        timeout: 5000,
        actions: [{ icon: 'close', color: 'white', round: true }],
      });
    }
  } finally {
    loading.value = false;
  }
}

function wasNotified(err: unknown) {
  return Boolean(err && typeof err === 'object' && '__anycodeNotified' in err);
}

function formatTime(value: string) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

watch(page, () => {
  void loadPage();
});

onMounted(() => {
  void loadPage();
});
</script>
