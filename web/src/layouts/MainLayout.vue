<template>
  <q-layout view="hHh lpR fFf" class="app-layout">
    <q-header v-if="applicationReady" bordered class="app-header">
      <q-toolbar class="app-toolbar">
        <q-btn
          v-if="$route.name !== 'overview'"
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
      :class="{ 'page-container--detail': $route.name === 'session-detail' }"
    >
      <router-view
        :key="$route.fullPath"
        @create-session="openNewSession"
        @session-title="sessionTitle = $event"
      />
    </q-page-container>

    <q-page-container v-else>
      <q-page class="flex flex-center">
        <q-spinner v-if="checkingProjects" color="primary" size="32px" aria-label="正在加载项目" />
      </q-page>
    </q-page-container>

    <q-page-sticky
      v-if="
        $route.name === 'overview' &&
        ($q.screen.width < overviewDesktopMinWidth || isOverviewHorizontalView)
      "
      v-show="applicationReady"
      position="bottom-right"
      :offset="[24, 24]"
    >
      <q-btn
        fab
        color="primary"
        class="app-on-primary"
        icon="add"
        aria-label="新建卡片"
        @click="openNewSession"
      >
        <q-tooltip>新建卡片</q-tooltip>
      </q-btn>
    </q-page-sticky>

    <new-session-dialog
      v-if="applicationReady && $route.name === 'overview'"
      v-model="newSessionOpen"
      :default-project-id="newSessionDefaultProjectId"
      :panel="showOverviewCreatePanel"
    />
    <GlobalSettingsDialog
      v-if="applicationReady && !$q.screen.lt.sm"
      v-model="settingsDialogOpen"
    />

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
import NewSessionDialog from '@/components/NewSessionDialog.vue';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useThemeMode } from '@/composables/useThemeMode';
import { clearGraphQLAccessKey } from '@/services/graphqlClient';
import { disablePushNotifications } from '@/services/pushNotifications';

const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const newSessionOpen = ref(false);
const settingsDialogOpen = ref(false);
const logoutDialogOpen = ref(false);
const { themeMode, themeModes } = useThemeMode();
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
const showOverviewCreatePanel = computed(
  () =>
    route.name === 'overview' &&
    $q.screen.width >= overviewDesktopMinWidth &&
    !isOverviewHorizontalView.value,
);
const isOverviewHorizontalView = computed(
  () =>
    route.name === 'overview' &&
    $q.screen.width >= overviewDesktopMinWidth &&
    route.query.view === 'horizontal',
);
const newSessionDefaultProjectId = computed(() => {
  const queryProjectId = route.query.projectId;
  return typeof queryProjectId === 'string' ? queryProjectId : '';
});
const sessionsRoute = computed(() => {
  const projectId = route.query.projectId;
  return typeof projectId === 'string'
    ? { name: 'sessions', query: { projectId, scope: 'closed' } }
    : { name: 'sessions', query: { scope: 'closed' } };
});

onMounted(() => {
  void loadProjects()
    .catch(() => undefined)
    .finally(() => {
      checkingProjects.value = false;
    });
});

watch(
  () => route.fullPath,
  () => {
    sessionTitle.value = '';
  },
);

watch(
  [checkingProjects, initialProjectRequired, () => $q.screen.lt.sm],
  ([checking, required, mobile]) => {
    if (checking || !required || !mobile || route.name === 'project-create') return;
    void router.replace({ name: 'project-create' });
  },
  { immediate: true },
);

function openNewSession() {
  if ($q.screen.lt.sm) {
    void router.push(
      newSessionDefaultProjectId.value
        ? { name: 'new-session', query: { projectId: newSessionDefaultProjectId.value } }
        : { name: 'new-session' },
    );
    return;
  }
  newSessionOpen.value = true;
}

function toggleOverviewView() {
  const query = { ...route.query };
  if (isOverviewHorizontalView.value) {
    delete query.view;
  } else {
    query.view = 'horizontal';
  }
  void router.replace({ name: 'overview', query });
}

function openSettings() {
  if ($q.screen.lt.sm) {
    void router.push({ name: 'settings' });
    return;
  }
  settingsDialogOpen.value = true;
}

async function logout() {
  await disablePushNotifications().catch(() => undefined);
  clearGraphQLAccessKey();
  logoutDialogOpen.value = false;
  await router.replace({ name: 'login' });
}
</script>
