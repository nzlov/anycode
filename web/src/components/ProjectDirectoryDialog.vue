<template>
  <q-dialog :model-value="modelValue" @update:model-value="emitModel">
    <q-card class="directory-dialog">
      <q-card-section class="row items-center q-pb-sm">
        <div>
          <div class="text-subtitle1 text-weight-bold">选择项目目录</div>
          <div class="text-caption text-muted">目录树由后端权限范围决定</div>
        </div>
        <q-space />
        <q-btn v-close-popup flat round dense icon="close" aria-label="关闭" />
      </q-card-section>

      <q-separator />

      <q-card-section>
        <q-input v-model="pathInput" dense outlined label="当前路径" @keyup.enter="goToInputPath">
          <template #prepend>
            <q-icon name="folder" />
          </template>
          <template #append>
            <q-btn flat round dense icon="keyboard_return" aria-label="打开路径" @click="goToInputPath" />
          </template>
        </q-input>

        <div class="row items-center q-gutter-sm q-mt-sm">
          <q-btn
            flat
            dense
            no-caps
            icon="arrow_upward"
            label="上一级"
            :disable="!parentPath || parentPath === currentPath"
            @click="goToPath(parentPath)"
          />
          <q-space />
          <q-input v-model="filter" dense borderless placeholder="过滤当前目录" clearable class="directory-filter">
            <template #prepend>
              <q-icon name="search" />
            </template>
          </q-input>
        </div>

        <q-banner v-if="error" dense rounded class="bg-red-1 text-negative q-mt-sm">
          {{ error }}
        </q-banner>

        <q-list bordered separator class="directory-list q-mt-sm">
          <q-item
            v-for="entry in filteredEntries"
            :key="entry.path"
            clickable
            :disable="!entry.isDir || !entry.canRead"
            :active="selected === entry.path"
            active-class="bg-primary-1 text-primary"
            @click="enterDirectory(entry)"
          >
            <q-item-section avatar>
              <q-icon :name="entry.isGit ? 'folder_open' : 'folder'" :color="entry.canRead ? 'primary' : 'grey-5'" />
            </q-item-section>
            <q-item-section>
              <q-item-label>{{ entry.name }}</q-item-label>
              <q-item-label caption lines="1">{{ entry.path }}</q-item-label>
            </q-item-section>
            <q-item-section side>
              <div class="row items-center q-gutter-xs">
                <q-chip v-if="entry.isGit" dense color="green-1" text-color="green-9" label="Git" />
                <q-chip v-if="!entry.canRead" dense color="red-1" text-color="negative" :label="entry.errorCode || '不可读'" />
                <q-btn
                  v-if="entry.isDir && entry.canRead"
                  flat
                  dense
                  round
                  icon="check"
                  aria-label="选择目录"
                  @click.stop="selectDirectory(entry.path)"
                />
              </div>
            </q-item-section>
          </q-item>
          <q-item v-if="!loading && filteredEntries.length === 0">
            <q-item-section class="text-muted">当前目录没有可显示条目</q-item-section>
          </q-item>
        </q-list>
        <q-inner-loading :showing="loading" />
      </q-card-section>

      <q-card-actions align="right">
        <q-btn v-close-popup flat color="primary" label="取消" no-caps />
        <q-btn
          unelevated
          color="primary"
          icon="folder_open"
          label="使用目录"
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
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
}>();

const filter = ref('');
const selected = ref('');
const pathInput = ref('/');
const creating = ref(false);
const { currentPath, parentPath, entries, error, loading, loadDirectory } = useDirectoryBrowser();
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
