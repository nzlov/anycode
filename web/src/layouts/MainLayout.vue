<template>
  <q-layout view="lHh Lpr lFf">
    <q-header bordered class="bg-white text-dark">
      <q-toolbar>
        <q-btn flat dense round icon="menu" aria-label="打开导航" @click="toggleLeftDrawer" />

        <q-toolbar-title class="text-weight-bold">AnyCode</q-toolbar-title>

        <q-badge outline color="primary" label="本机 Codex" />
      </q-toolbar>
    </q-header>

    <q-drawer v-model="leftDrawerOpen" show-if-above :width="280" class="app-drawer">
      <q-list padding>
        <q-item-label header class="text-blue-grey-3">Codex agent 工作台</q-item-label>

        <q-item clickable active>
          <q-item-section avatar>
            <q-icon name="space_dashboard" />
          </q-item-section>
          <q-item-section>
            <q-item-label>总揽</q-item-label>
            <q-item-label caption class="text-blue-grey-4">全部项目卡片</q-item-label>
          </q-item-section>
        </q-item>

        <q-separator dark spaced />

        <q-item-label header class="text-blue-grey-3">项目</q-item-label>
        <q-item v-for="project in projects" :key="project.path" clickable>
          <q-item-section avatar>
            <q-icon name="folder_open" :color="project.active ? 'positive' : 'blue-grey-4'" />
          </q-item-section>
          <q-item-section>
            <q-item-label>{{ project.name }}</q-item-label>
            <q-item-label caption class="text-blue-grey-4">{{ project.path }}</q-item-label>
          </q-item-section>
          <q-item-section side>
            <q-btn flat round dense color="blue-grey-3" icon="settings" aria-label="项目设置" />
          </q-item-section>
        </q-item>
      </q-list>
    </q-drawer>

    <q-page-container>
      <router-view />
    </q-page-container>
  </q-layout>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const projects = [
  {
    name: 'openchamber',
    path: '/workspaces/openchamber',
    active: true,
  },
  {
    name: 'pets',
    path: '/workspaces/pets',
    active: false,
  },
  {
    name: 'anycode',
    path: '/workspaces/anycode',
    active: false,
  },
];

const leftDrawerOpen = ref(false);

function toggleLeftDrawer() {
  leftDrawerOpen.value = !leftDrawerOpen.value;
}
</script>
