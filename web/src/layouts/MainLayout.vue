<template>
  <q-layout view="hHh lpR fFf" class="app-layout">
    <q-header v-if="applicationReady" bordered class="app-header">
      <q-toolbar
        class="app-toolbar"
        :class="{ 'app-toolbar--overview': $route.name === 'overview' }"
      >
        <q-btn
          v-if="isContentRoute"
          flat
          round
          dense
          class="app-icon-btn"
          icon="arrow_back"
          aria-label="返回上一页"
          @click="goBackFromContent"
        >
          <q-tooltip>返回</q-tooltip>
        </q-btn>
        <q-btn
          v-else-if="$route.name !== 'overview'"
          flat
          round
          dense
          class="app-icon-btn"
          icon="img:/icons/anycode.svg"
          aria-label="返回总览"
          :to="{ name: 'overview' }"
        >
          <q-tooltip>总览</q-tooltip>
        </q-btn>
        <q-toolbar-title v-if="$route.name === 'session-detail'" class="app-header__title">
          {{ sessionTitle || '会话详情' }}
        </q-toolbar-title>
        <div
          v-show="$route.name !== 'session-detail'"
          id="app-page-toolbar"
          class="app-page-toolbar-host"
        />

        <q-btn
          v-if="$route.name === 'overview'"
          flat
          round
          dense
          class="app-icon-btn"
          icon="history"
          aria-label="历史卡片"
          :to="sessionsRoute"
        >
          <q-tooltip>历史卡片</q-tooltip>
        </q-btn>
        <q-btn
          v-if="$route.name === 'overview'"
          flat
          round
          dense
          class="app-icon-btn"
          icon="folder"
          aria-label="项目管理"
          :to="{ name: 'projects' }"
        >
          <q-tooltip>项目管理</q-tooltip>
        </q-btn>
        <q-btn
          v-if="$route.name === 'overview' && runningTunnelCount > 0"
          flat
          round
          dense
          class="app-icon-btn"
          icon="lan"
          aria-label="隧道"
          @click="openTunnels"
        >
          <q-badge floating rounded color="negative">{{ runningTunnelCount }}</q-badge>
          <q-tooltip>隧道</q-tooltip>
        </q-btn>
        <q-btn
          v-if="$route.name === 'overview' && $q.screen.width >= overviewDesktopMinWidth"
          flat
          round
          dense
          class="app-icon-btn"
          :icon="isOverviewHorizontalView ? 'grid_view' : 'view_column'"
          :aria-label="isOverviewHorizontalView ? '切换卡片视图' : '切换横向视图'"
          :aria-pressed="isOverviewHorizontalView"
          @click="toggleOverviewView"
        >
          <q-tooltip>{{ isOverviewHorizontalView ? '卡片视图' : '横向视图' }}</q-tooltip>
        </q-btn>
        <q-btn flat round dense class="app-icon-btn" icon="more_vert" aria-label="更多操作">
          <q-badge
            v-if="availableRelease"
            floating
            rounded
            color="negative"
            aria-label="有新版本"
          />
          <q-menu>
            <q-list dense class="app-touch-list">
              <q-item v-close-popup clickable @click="openSettings">
                <q-item-section avatar>
                  <q-icon name="settings" />
                </q-item-section>
                <q-item-section>全局设置</q-item-section>
              </q-item>
              <q-separator />
              <q-item-label header>主题模式</q-item-label>
              <q-item
                v-for="mode in themeModes"
                :key="mode.value"
                v-close-popup
                clickable
                :active="themeMode === mode.value"
                @click="themeMode = mode.value"
              >
                <q-item-section avatar>
                  <q-icon :name="mode.icon" />
                </q-item-section>
                <q-item-section>{{ mode.label }}</q-item-section>
              </q-item>
              <q-separator />
              <q-item
                v-if="availableRelease"
                v-close-popup
                clickable
                @click="updateDialogOpen = true"
              >
                <q-item-section avatar>
                  <q-icon name="new_releases" color="primary" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>发现新版本 {{ availableRelease.version }}</q-item-label>
                  <q-item-label caption>当前版本 {{ currentVersion }}</q-item-label>
                </q-item-section>
              </q-item>
              <q-item v-else>
                <q-item-section avatar>
                  <q-icon name="info" />
                </q-item-section>
                <q-item-section>
                  <q-item-label>当前版本</q-item-label>
                  <q-item-label caption>{{ currentVersion }}</q-item-label>
                </q-item-section>
              </q-item>
              <q-separator />
              <q-item
                v-close-popup
                clickable
                class="text-negative"
                @click="logoutDialogOpen = true"
              >
                <q-item-section avatar>
                  <q-icon name="logout" />
                </q-item-section>
                <q-item-section>退出</q-item-section>
              </q-item>
            </q-list>
          </q-menu>
        </q-btn>
      </q-toolbar>
    </q-header>

    <q-page-container
      v-if="applicationReady"
      :class="{
        'page-container--detail': $route.name === 'session-detail',
        'page-container--horizontal': isOverviewHorizontalView,
      }"
    >
      <!-- GLUE: the overview owns tunnel details; only its count crosses into the layout toolbar. -->
      <router-view
        :key="$route.fullPath"
        @session-title="sessionTitle = $event"
        @tunnel-count="runningTunnelCount = $event"
      />
    </q-page-container>

    <q-page-container v-else>
      <q-page class="flex flex-center">
        <q-spinner v-if="checkingProjects" color="primary" size="32px" aria-label="正在加载项目" />
      </q-page>
    </q-page-container>

    <GlobalSettingsDialog
      v-if="applicationReady && !$q.screen.lt.sm"
      v-model="settingsDialogOpen"
    />
    <TunnelManagerDialog v-if="applicationReady && !$q.screen.lt.sm" v-model="tunnelDialogOpen" />

    <q-dialog v-if="availableRelease" v-model="updateDialogOpen">
      <q-card class="update-dialog app-content-dialog">
        <q-card-section class="row items-start no-wrap">
          <div class="col">
            <div class="text-subtitle1 text-weight-bold">{{ availableRelease.name }}</div>
            <div class="text-caption text-secondary">
              {{ availableRelease.version }} · 当前 {{ currentVersion }}
              <template v-if="availableReleasePublishedAt">
                · {{ availableReleasePublishedAt }}
              </template>
            </div>
          </div>
          <q-btn
            v-close-popup
            flat
            round
            dense
            class="app-icon-btn"
            icon="close"
            aria-label="关闭更新内容"
          />
        </q-card-section>
        <q-separator />
        <q-card-section class="update-dialog__body">
          <MarkdownContent :text="availableRelease.body" />
        </q-card-section>
        <q-separator />
        <q-card-actions align="right">
          <q-btn
            flat
            no-caps
            color="primary"
            icon-right="open_in_new"
            label="查看 GitHub Release"
            :href="availableRelease.url"
            target="_blank"
            rel="noopener noreferrer"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>

    <q-dialog v-if="applicationReady" v-model="logoutDialogOpen">
      <q-card class="confirm-dialog">
        <q-card-section>
          <div class="text-subtitle1 text-weight-bold">退出登录</div>
        </q-card-section>
        <q-card-actions align="right">
          <q-btn
            v-close-popup
            flat
            round
            class="app-icon-btn"
            icon="close"
            color="primary"
            aria-label="取消"
          >
            <q-tooltip>取消</q-tooltip>
          </q-btn>
          <q-btn
            unelevated
            class="app-command-btn"
            color="negative"
            icon="logout"
            label="退出"
            no-caps
            @click="logout"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>
    <ProjectDirectoryDialog
      v-if="!$q.screen.lt.sm"
      :model-value="initialProjectRequired"
      :persistent="initialProjectRequired"
    />
  </q-layout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import GlobalSettingsDialog from '@/components/GlobalSettingsDialog.vue';
import MarkdownContent from '@/components/MarkdownContent.vue';
import { provideGeneralSettingsInvalidation } from '@/composables/useGeneralSettingsInvalidation';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import TunnelManagerDialog from '@/components/TunnelManagerDialog.vue';
import { useOverviewViewMode } from '@/composables/useOverviewViewMode';
import { useProjects } from '@/composables/useProjects';
import { useThemeMode } from '@/composables/useThemeMode';
import { clearGraphQLAccessKey } from '@/services/graphqlClient';
import { getAppVersionStatus, type AppRelease } from '@/services/appVersion';
import { disablePushNotifications } from '@/services/pushNotifications';
import { provideAnnotationDraftInjector } from '@/services/annotationDraftInjection';

const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const settingsDialogOpen = ref(false);
// GLUE: carry only invalidation semantics so route pages re-query the canonical settings.
// Remove this when general settings changes have a backend subscription.
provideGeneralSettingsInvalidation();
const tunnelDialogOpen = ref(false);
const logoutDialogOpen = ref(false);
const updateDialogOpen = ref(false);
const currentVersion = ref('dev');
const availableRelease = ref<AppRelease | null>(null);
const runningTunnelCount = ref(0);
const { themeMode, themeModes } = useThemeMode();
const { overviewViewMode } = useOverviewViewMode();
const route = useRoute();
const router = useRouter();
const sessionTitle = ref('');
const checkingProjects = ref(true);
const { projects, loaded: projectsLoaded, loadProjects } = useProjects();
const initialProjectRequired = computed(
  () => !checkingProjects.value && projectsLoaded.value && projects.value.length === 0,
);
const applicationReady = computed(
  () =>
    !checkingProjects.value &&
    (!initialProjectRequired.value || route.name === 'project-create'),
);
const isContentRoute = computed(() =>
  ['diff', 'session-artifacts', 'session-artifact'].includes(String(route.name ?? '')),
);
const isOverviewHorizontalView = computed(
  () =>
    route.name === 'overview' &&
    $q.screen.width >= overviewDesktopMinWidth &&
    overviewViewMode.value === 'horizontal',
);
const sessionsRoute = computed(() => {
  const projectId = route.query.projectId;
  return typeof projectId === 'string'
    ? { name: 'sessions', query: { projectId, scope: 'closed' } }
    : { name: 'sessions', query: { scope: 'closed' } };
});
const availableReleasePublishedAt = computed(() => {
  if (!availableRelease.value?.publishedAt) return '';
  const publishedAt = new Date(availableRelease.value.publishedAt);
  if (Number.isNaN(publishedAt.getTime())) return '';
  return publishedAt.toLocaleDateString();
});

// GLUE: standalone preview routes hand one transient annotation attachment to the owning session composer.
// Remove when those previews live inside the session detail route.
provideAnnotationDraftInjector({
  canInject: (sessionId) => Boolean(sessionId || annotationRouteSessionId()),
  inject: (attachment, sessionId) => {
    const targetSessionId = sessionId || annotationRouteSessionId();
    if (!targetSessionId || !attachment.content.trim()) return;
    void router.push({
      name: 'session-detail',
      params: { id: targetSessionId },
      state: { annotationAttachment: JSON.stringify(attachment) },
    });
  },
});

function annotationRouteSessionId() {
  if (route.name === 'session-artifacts' || route.name === 'session-artifact') {
    return String(route.params.id ?? '');
  }
  if (route.name === 'diff') {
    return typeof route.query.sessionId === 'string' ? route.query.sessionId : '';
  }
  return '';
}

onMounted(() => {
  void loadProjects()
    .catch(() => undefined)
    .finally(() => {
      checkingProjects.value = false;
    });
  void getAppVersionStatus()
    .then((status) => {
      currentVersion.value = status.currentVersion;
      availableRelease.value = status.availableRelease;
    })
    .catch(() => undefined);
});

watch(
  () => route.fullPath,
  () => {
    sessionTitle.value = '';
  },
);

watch(
  () => [route.name, route.query.view] as const,
  ([routeName, view]) => {
    if (routeName === 'overview' && view === 'horizontal') {
      overviewViewMode.value = 'horizontal';
    }
  },
  { immediate: true },
);

watch(
  [checkingProjects, initialProjectRequired, () => $q.screen.lt.sm],
  ([checking, required, mobile]) => {
    if (checking || !required || !mobile || route.name === 'project-create') return;
    void router.replace({ name: 'project-create' });
  },
  { immediate: true },
);

function toggleOverviewView() {
  const query = { ...route.query };
  if (isOverviewHorizontalView.value) {
    overviewViewMode.value = 'card';
    delete query.view;
  } else {
    overviewViewMode.value = 'horizontal';
    query.view = 'horizontal';
  }
  void router.replace({ name: 'overview', query });
}

function goBackFromContent() {
  if (typeof window.history.state?.back === 'string' && window.history.state.back) {
    router.back();
    return;
  }
  if (route.name === 'session-artifact') {
    void router.replace({ name: 'session-artifacts', params: { id: route.params.id } });
    return;
  }
  const sessionId = route.name === 'session-artifacts' ? route.params.id : route.query.sessionId;
  if (sessionId) {
    void router.replace({ name: 'session-detail', params: { id: String(sessionId) } });
    return;
  }
  void router.replace({ name: 'overview' });
}

function openSettings() {
  if ($q.screen.lt.sm) {
    void router.push({ name: 'settings' });
    return;
  }
  settingsDialogOpen.value = true;
}

function openTunnels() {
  if ($q.screen.lt.sm) {
    void router.push({ name: 'tunnels' });
    return;
  }
  tunnelDialogOpen.value = true;
}

async function logout() {
  await disablePushNotifications().catch(() => undefined);
  clearGraphQLAccessKey();
  logoutDialogOpen.value = false;
  await router.replace({ name: 'login' });
}
</script>

<style scoped>
.update-dialog {
  width: min(680px, calc(100vw - 24px));
  max-height: calc(100dvh - 24px);
  display: flex;
  flex-direction: column;
}

.update-dialog__body {
  overflow-y: auto;
}
</style>
