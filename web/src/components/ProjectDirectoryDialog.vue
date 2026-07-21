<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    :persistent="persistent"
    @update:model-value="emitModel"
  >
    <q-card class="directory-dialog app-content-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div class="text-subtitle1 text-weight-bold">选择项目目录</div>
        <q-space />
        <q-btn v-if="!persistent" v-close-popup flat round dense icon="close" aria-label="关闭">
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>

      <q-separator />

      <q-card-section class="directory-dialog__body">
        <q-input v-model="pathInput" dense outlined label="当前路径" @keyup.enter="goToInputPath">
          <template #prepend>
            <q-icon name="folder" />
          </template>
          <template #append>
            <q-btn flat round dense icon="keyboard_return" aria-label="打开路径" @click="goToInputPath">
              <q-tooltip>打开路径</q-tooltip>
            </q-btn>
          </template>
        </q-input>

        <div class="directory-breadcrumb">
          <q-btn
            outline
            dense
            icon="arrow_upward"
            label="上一级"
            aria-label="上一级"
            :disable="!parentPath || parentPath === currentPath"
            @click="goToPath(parentPath)"
          >
            <q-tooltip>上一级</q-tooltip>
          </q-btn>
          <span class="text-body2 text-muted directory-breadcrumb__path">{{ currentPath }}</span>
          <q-space />
          <q-input v-model="filter" dense borderless placeholder="过滤当前目录" clearable class="directory-filter">
            <template #prepend>
              <q-icon name="search" />
            </template>
          </q-input>
        </div>

        <q-list bordered separator class="directory-list">
          <q-item
            v-for="entry in filteredEntries"
            :key="entry.path"
            clickable
            :disable="!entry.isDir || !entry.canRead"
            :active="selected === entry.path"
            active-class="directory-entry--active"
            @click="enterDirectory(entry)"
          >
            <q-item-section avatar>
              <q-icon
                :name="entry.isGit ? 'folder_open' : 'folder'"
                :class="entry.canRead ? 'text-primary' : 'text-muted'"
              />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ entry.name }}</q-item-label>
              <q-item-label caption lines="1">{{ entry.path }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <div class="row items-center q-gutter-xs">
                <q-chip v-if="entry.isGit" dense class="directory-entry__git" label="Git" />
                <q-chip
                  v-if="!entry.canRead"
                  dense
                  class="directory-entry__error"
                  :label="entry.errorCode || '不可读'"
                />
                <q-btn
                  v-if="entry.isDir && entry.canRead"
                  flat
                  dense
                  round
                  icon="check"
                  aria-label="选择目录"
                  @click.stop="selectDirectory(entry.path)"
                >
                  <q-tooltip>选择目录</q-tooltip>
                </q-btn>
              </div>
            </q-item-section>
          </q-item>
          <q-item v-if="!loading && filteredEntries.length === 0">
            <q-item-section class="text-muted">当前目录没有可显示条目</q-item-section>
          </q-item>
        </q-list>
        <q-card flat bordered class="selected-directory-card">
          <div>
            <div class="text-caption text-muted">当前选择</div>
            <div class="mono selected-directory-card__path">{{ selected || '尚未选择目录' }}</div>
          </div>
        </q-card>
        <q-inner-loading :showing="loading" />
      </q-card-section>

      <q-card-actions class="directory-dialog__actions">
        <q-btn v-if="!persistent" v-close-popup flat round color="primary" icon="close" aria-label="取消">
          <q-tooltip>取消</q-tooltip>
        </q-btn>
        <q-btn
          unelevated
          color="primary"
          class="app-on-primary"
          icon="folder_open"
          label="打开该项目"
          no-caps
          :loading="creating"
          :disable="!selected"
          @click="useSelectedDirectory"
        />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import { useDirectoryBrowser } from '@/composables/useDirectoryBrowser';
import type { DirectoryEntry } from '@/services/projects';
import { useProjects } from '@/composables/useProjects';

const props = defineProps<{
  modelValue: boolean;
  persistent?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const filter = ref('');
const selected = ref('');
const pathInput = ref('/');
const creating = ref(false);
const { currentPath, parentPath, entries, loading, loadDirectory } = useDirectoryBrowser();
const { createProjectFromPath } = useProjects();

const filteredEntries = computed(() => {
  const keyword = filter.value.trim().toLowerCase();
  if (!keyword) return entries.value;
  return entries.value.filter((entry) => entry.name.toLowerCase().includes(keyword) || entry.path.toLowerCase().includes(keyword));
});

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      selected.value = '';
      void goToPath(pathInput.value || '/');
    }
  },
);

watch(currentPath, (path) => {
  pathInput.value = path;
});

function emitModel(value: boolean) {
  emit('update:modelValue', value);
}

async function useSelectedDirectory() {
  if (!selected.value) return;
  creating.value = true;
  try {
    await createProjectFromPath(selected.value);
    emit('update:modelValue', false);
  } finally {
    creating.value = false;
  }
}

async function goToInputPath() {
  await goToPath(pathInput.value || '/');
}

async function goToPath(path: string) {
  await loadDirectory(path);
}

async function enterDirectory(entry: DirectoryEntry) {
  if (!entry.isDir || !entry.canRead) return;
  selected.value = entry.path;
  await goToPath(entry.path);
}

function selectDirectory(path: string) {
  selected.value = path;
}
</script>

<style scoped>
.directory-entry--active {
  color: var(--ac-link);
  background: var(--ac-surface-selected);
}

.directory-entry__git {
  color: var(--ac-status-success-text);
  background: var(--ac-status-success-bg);
  border: 1px solid var(--ac-status-success-border);
}

.directory-entry__error {
  color: var(--ac-status-danger-text);
  background: var(--ac-status-danger-bg);
  border: 1px solid var(--ac-status-danger-border);
}
</style>
