<template>
  <q-layout view="lHh Lpr lFf" class="app-layout">
    <q-header bordered class="app-header">
      <q-toolbar>
        <q-btn flat dense round icon="menu" aria-label="打开导航" @click="toggleLeftDrawer" />

        <q-toolbar-title class="app-title">AnyCode</q-toolbar-title>

        <q-btn
          flat
          round
          dense
          icon="create_new_folder"
          aria-label="选择项目目录"
          @click="directoryDialogOpen = true"
        >
          <q-tooltip>选择项目目录</q-tooltip>
        </q-btn>

        <q-btn-dropdown flat dense round icon="palette" aria-label="主题模式">
          <q-list dense>
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
          </q-list>
        </q-btn-dropdown>
      </q-toolbar>
    </q-header>

    <q-drawer v-model="leftDrawerOpen" show-if-above :width="288" class="app-drawer">
      <q-scroll-area class="fit">
        <q-list padding>
          <q-item-label header class="drawer-header">Codex agent 工作台</q-item-label>

          <q-item clickable :active="$route.name === 'overview'" to="/">
            <q-item-section avatar>
              <q-icon name="space_dashboard" />
            </q-item-section>
            <q-item-section>
              <q-item-label>总揽</q-item-label>
              <q-item-label caption>全部项目卡片</q-item-label>
            </q-item-section>
          </q-item>

          <q-item clickable :active="$route.name === 'sessions'" to="/sessions">
            <q-item-section avatar>
              <q-icon name="table_rows" />
            </q-item-section>
            <q-item-section>
              <q-item-label>会话表格</q-item-label>
              <q-item-label caption>后端分页入口</q-item-label>
            </q-item-section>
          </q-item>

          <q-separator spaced />

          <q-item-label header class="drawer-header">项目</q-item-label>
          <q-item v-for="project in projects" :key="project.id" clickable>
            <q-item-section avatar>
              <q-icon name="folder_open" :color="project.active ? 'positive' : undefined" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ project.name }}</q-item-label>
              <q-item-label caption>{{ project.path }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <q-btn flat round dense icon="more_vert" aria-label="项目设置" @click.stop>
                <q-menu>
                  <q-list dense class="project-menu">
                    <q-item clickable :to="`/projects/${project.id}/workflow`">
                      <q-item-section avatar>
                        <q-icon name="account_tree" />
                      </q-item-section>
                      <q-item-section>流程配置</q-item-section>
                    </q-item>
                    <q-item clickable @click="directoryDialogOpen = true">
                      <q-item-section avatar>
                        <q-icon name="folder" />
                      </q-item-section>
                      <q-item-section>目录选择</q-item-section>
                    </q-item>
                  </q-list>
                </q-menu>
              </q-btn>
            </q-item-section>
          </q-item>
        </q-list>
      </q-scroll-area>
    </q-drawer>

    <q-page-container>
      <router-view @create-session="newSessionOpen = true" />
    </q-page-container>

    <q-page-sticky position="bottom-right" :offset="[24, 24]">
      <q-btn fab color="primary" icon="add" aria-label="新建卡片" @click="newSessionOpen = true" />
    </q-page-sticky>

    <new-session-dialog v-model="newSessionOpen" />
    <project-directory-dialog v-model="directoryDialogOpen" />
  </q-layout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue';

import NewSessionDialog from '@/components/NewSessionDialog.vue';
import ProjectDirectoryDialog from '@/components/ProjectDirectoryDialog.vue';
import { useProjects } from '@/composables/useProjects';
import { useThemeMode } from '@/composables/useThemeMode';

const leftDrawerOpen = ref(false);
const newSessionOpen = ref(false);
const directoryDialogOpen = ref(false);
const { themeMode, themeModes } = useThemeMode();
const { projects, loadProjects } = useProjects();

onMounted(() => {
  void loadProjects();
});

function toggleLeftDrawer() {
  leftDrawerOpen.value = !leftDrawerOpen.value;
}
</script>
