<template>
  <q-layout view="hHh lpR fFf" class="app-layout">
    <q-header bordered class="app-header">
      <q-toolbar>
        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="space_dashboard"
          aria-label="返回总揽"
          :to="{ name: 'overview' }"
        >
          <q-tooltip>总揽</q-tooltip>
        </q-btn>
        <q-space />

        <q-btn flat round dense class="app-icon-btn" icon="more_vert" aria-label="更多操作">
          <q-menu>
            <q-list dense class="app-touch-list">
              <q-item v-close-popup clickable @click="settingsDialogOpen = true">
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

    <q-page-container :class="{ 'page-container--detail': $route.name === 'session-detail' }">
      <router-view
        :key="`${$route.fullPath}:${pageRefreshKey}`"
        @create-session="newSessionOpen = true"
      />
    </q-page-container>

    <q-page-sticky
      v-if="$route.name === 'overview' && $q.screen.width < overviewDesktopMinWidth"
      position="bottom-right"
      :offset="[24, 24]"
    >
      <q-btn
        fab
        color="positive"
        text-color="dark"
        icon="add"
        aria-label="新建卡片"
        @click="newSessionOpen = true"
      >
        <q-tooltip>新建卡片</q-tooltip>
      </q-btn>
    </q-page-sticky>

    <new-session-dialog
      v-if="$route.name === 'overview'"
      v-model="newSessionOpen"
      :default-project-id="newSessionDefaultProjectId"
      :panel="showOverviewCreatePanel"
      @create="handleSessionCreated"
    />
    <GlobalSettingsDialog v-model="settingsDialogOpen" />

    <q-dialog v-model="logoutDialogOpen">
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
  </q-layout>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import GlobalSettingsDialog from '@/components/GlobalSettingsDialog.vue';
import NewSessionDialog from '@/components/NewSessionDialog.vue';
import { useThemeMode } from '@/composables/useThemeMode';
import { clearGraphQLAccessKey } from '@/services/graphqlClient';

const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const newSessionOpen = ref(false);
const settingsDialogOpen = ref(false);
const logoutDialogOpen = ref(false);
const { themeMode, themeModes } = useThemeMode();
const route = useRoute();
const router = useRouter();
const pageRefreshKey = ref(0);
const showOverviewCreatePanel = computed(
  () => route.name === 'overview' && $q.screen.width >= overviewDesktopMinWidth,
);
const newSessionDefaultProjectId = computed(() => {
  const queryProjectId = route.query.projectId;
  return typeof queryProjectId === 'string' ? queryProjectId : '';
});

function handleSessionCreated() {
  pageRefreshKey.value += 1;
}

async function logout() {
  clearGraphQLAccessKey();
  logoutDialogOpen.value = false;
  await router.replace({ name: 'login' });
}
</script>
