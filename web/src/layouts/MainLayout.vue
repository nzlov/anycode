<template>
  <q-layout view="lHh Lpr lFf" class="app-layout">
    <q-header bordered class="app-header">
      <q-toolbar>
        <q-btn
          flat
          dense
          round
          class="lt-md app-icon-btn"
          icon="menu"
          aria-label="打开导航"
          @click="toggleLeftDrawer"
        >
          <q-tooltip>打开导航</q-tooltip>
        </q-btn>

        <q-toolbar-title class="app-title">AnyCode</q-toolbar-title>

        <q-btn
          flat
          round
          dense
          class="app-icon-btn"
          icon="create_new_folder"
          aria-label="选择项目目录"
          @click="directoryDialogOpen = true"
        >
          <q-tooltip>选择项目目录</q-tooltip>
        </q-btn>

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

    <q-drawer v-model="leftDrawerOpen" show-if-above :width="288" class="app-drawer">
      <q-scroll-area class="fit">
        <q-list padding>
          <q-item-label header class="drawer-header">Codex agent 工作台</q-item-label>

          <q-item clickable :active="$route.name === 'overview' && !$route.query.projectId" to="/">
            <q-item-section avatar>
              <q-icon name="space_dashboard" />
            </q-item-section>
            <q-item-section>
              <q-item-label>总揽</q-item-label>
              <q-item-label caption>全部项目卡片</q-item-label>
            </q-item-section>
          </q-item>

          <q-separator spaced />

          <q-item-label header class="drawer-header">项目</q-item-label>
          <q-item
            v-for="project in projects"
            :key="project.id"
            clickable
            :active="projectActive(project.id)"
            @click="$router.push({ name: 'overview', query: { projectId: project.id } })"
          >
            <q-item-section avatar>
              <q-icon
                name="folder_open"
                :color="projectActive(project.id) ? 'positive' : undefined"
              />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ project.name }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-btn
                flat
                round
                dense
                class="app-icon-btn"
                icon="more_vert"
                aria-label="项目设置"
                @click.stop.prevent
              >
                <q-menu>
                  <q-list dense class="project-menu app-touch-list">
                    <q-item v-close-popup clickable @click.stop="openProjectSettings(project)">
                      <q-item-section avatar>
                        <q-icon name="settings" />
                      </q-item-section>
                      <q-item-section>设置</q-item-section>
                    </q-item>
                    <q-item v-close-popup clickable :to="`/projects/${project.id}/workflow`">
                      <q-item-section avatar>
                        <q-icon name="account_tree" />
                      </q-item-section>
                      <q-item-section>流程配置</q-item-section>
                    </q-item>
                    <q-item
                      v-close-popup
                      clickable
                      class="text-negative"
                      @click.stop="confirmRemoveProject(project.id, project.name)"
                    >
                      <q-item-section avatar>
                        <q-icon name="playlist_remove" />
                      </q-item-section>
                      <q-item-section>移除项目</q-item-section>
                    </q-item>
                  </q-list>
                </q-menu>
              </q-btn>
            </q-item-section>
          </q-item>
        </q-list>
      </q-scroll-area>
    </q-drawer>

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
    <project-directory-dialog v-model="directoryDialogOpen" />
    <GlobalSettingsDialog v-model="settingsDialogOpen" />
    <project-settings-dialog v-model="projectSettingsOpen" :project="settingsProject" />

    <q-dialog v-model="removeProjectDialogOpen">
      <q-card class="confirm-dialog">
        <q-card-section class="row items-center q-pb-sm">
          <div class="text-subtitle1 text-weight-bold">移除项目</div>
          <q-space />
          <q-btn v-close-popup flat round dense class="app-icon-btn" icon="close" aria-label="关闭">
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </q-card-section>

        <q-separator />

        <q-card-section>
          <div class="text-body2">确认移除项目“{{ removingProjectName }}”？</div>
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
            icon="playlist_remove"
            label="移除"
            no-caps
            :loading="removingProject"
            @click="removeSelectedProject"
          />
        </q-card-actions>
      </q-card>
    </q-dialog>

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
import { computed, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { useRoute, useRouter } from 'vue-router';

import GlobalSettingsDialog from '@/components/GlobalSettingsDialog.vue';
import NewSessionDialog from '@/components/NewSessionDialog.vue';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import ProjectSettingsDialog from '@/components/ProjectSettingsDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useThemeMode } from '@/composables/useThemeMode';
import { clearGraphQLAccessKey } from '@/services/graphqlClient';
import { getSession } from '@/services/sessions';
import type { ProjectSummary } from '@/services/projects';

const leftDrawerOpen = ref(false);
const $q = useQuasar();
const overviewDesktopMinWidth = 700;
const newSessionOpen = ref(false);
const directoryDialogOpen = ref(false);
const settingsDialogOpen = ref(false);
const projectSettingsOpen = ref(false);
const settingsProject = ref<ProjectSummary | null>(null);
const removeProjectDialogOpen = ref(false);
const removingProjectId = ref('');
const removingProjectName = ref('');
const removingProject = ref(false);
const logoutDialogOpen = ref(false);
const { themeMode, themeModes } = useThemeMode();
const { projects, loadProjects, removeProjectById } = useProjects();
const route = useRoute();
const router = useRouter();
const activeProjectId = ref('');
const pageRefreshKey = ref(0);
const showOverviewCreatePanel = computed(
  () => route.name === 'overview' && $q.screen.width >= overviewDesktopMinWidth,
);
const newSessionDefaultProjectId = computed(() => {
  const projectId = route.params.projectId;
  if (typeof projectId === 'string') return projectId;
  const queryProjectId = route.query.projectId;
  return typeof queryProjectId === 'string' ? queryProjectId : '';
});

onMounted(() => {
  void loadProjects();
  void refreshActiveProject();
});

watch(
  () => route.fullPath,
  () => {
    void refreshActiveProject();
  },
);

function toggleLeftDrawer() {
  leftDrawerOpen.value = !leftDrawerOpen.value;
}

function handleSessionCreated() {
  pageRefreshKey.value += 1;
}

function projectActive(projectId: string) {
  return activeProjectId.value === projectId;
}

async function refreshActiveProject() {
  const queryProjectId = route.query.projectId;
  if (typeof queryProjectId === 'string') {
    activeProjectId.value = queryProjectId;
    return;
  }
  const paramProjectId = route.params.projectId;
  if (typeof paramProjectId === 'string') {
    activeProjectId.value = paramProjectId;
    return;
  }
  if (route.name === 'session-detail' || route.name === 'session-commits') {
    const sessionId = route.params.id;
    if (typeof sessionId === 'string') {
      const detail = await getSession(sessionId);
      activeProjectId.value = detail.projectId;
      return;
    }
  }
  if (route.name === 'diff') {
    const diffSessionId = route.query.sessionId;
    if (typeof diffSessionId === 'string') {
      const detail = await getSession(diffSessionId);
      activeProjectId.value = detail.projectId;
      return;
    }
  }
  activeProjectId.value = '';
}

function confirmRemoveProject(projectId: string, projectName: string) {
  removingProjectId.value = projectId;
  removingProjectName.value = projectName;
  removeProjectDialogOpen.value = true;
}

function openProjectSettings(project: ProjectSummary) {
  settingsProject.value = project;
  projectSettingsOpen.value = true;
}

async function removeSelectedProject() {
  if (!removingProjectId.value) return;
  removingProject.value = true;
  try {
    await removeProjectById(removingProjectId.value);
    removeProjectDialogOpen.value = false;
    if (activeProjectId.value === removingProjectId.value) {
      await router.push({ name: 'overview' });
    }
  } finally {
    removingProject.value = false;
  }
}

async function logout() {
  clearGraphQLAccessKey();
  logoutDialogOpen.value = false;
  await router.replace({ name: 'login' });
}
</script>
