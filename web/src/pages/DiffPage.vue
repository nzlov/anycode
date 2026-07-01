<template>
  <q-page class="page-shell">
    <div class="page-heading">
      <div>
        <div class="text-h5 text-weight-bold">当前分支变更</div>
        <div class="text-body2 text-muted">按卡片 worktree 实时计算的 Diff 页面骨架</div>
      </div>
      <q-btn-toggle
        v-model="viewMode"
        no-caps
        toggle-color="primary"
        :options="[
          { label: '单个文件', value: 'single', icon: 'description' },
          { label: '全部 Diff', value: 'all', icon: 'difference' },
        ]"
      />
    </div>

    <div class="diff-layout">
      <q-card flat bordered class="diff-files">
        <q-list separator>
          <q-item
            v-for="file in diffFiles"
            :key="file.path"
            clickable
            :active="selectedPath === file.path"
            @click="selectedPath = file.path"
          >
            <q-item-section avatar>
              <q-icon :name="fileIcon(file.status)" :color="fileColor(file.status)" />
            </q-item-section>
            <q-item-section>
              <q-item-label class="ellipsis">{{ file.path }}</q-item-label>
              <q-item-label caption>+{{ file.additions }} / -{{ file.deletions }}</q-item-label>
            </q-item-section>
          </q-item>
        </q-list>
      </q-card>

      <section class="diff-content">
        <q-card v-for="file in visibleFiles" :key="file.path" flat bordered class="diff-file-card">
          <q-card-section class="diff-file-header">
            <div class="text-weight-medium">{{ file.path }}</div>
            <q-badge outline :color="fileColor(file.status)" :label="file.status" />
          </q-card-section>
          <q-separator />
          <q-card-section class="diff-code">
            <pre v-for="line in file.hunks" :key="line" :class="lineClass(line)">{{ line }}</pre>
          </q-card-section>
        </q-card>
      </section>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';

import { diffFiles } from '@/mocks/workbench';

const viewMode = ref<'single' | 'all'>('single');
const selectedPath = ref(diffFiles[0]?.path ?? '');

const visibleFiles = computed(() => {
  if (viewMode.value === 'all') {
    return diffFiles;
  }
  return diffFiles.filter((file) => file.path === selectedPath.value);
});

function fileIcon(status: (typeof diffFiles)[number]['status']) {
  return status === 'added' ? 'add_circle' : status === 'deleted' ? 'remove_circle' : 'edit';
}

function fileColor(status: (typeof diffFiles)[number]['status']) {
  return status === 'added' ? 'positive' : status === 'deleted' ? 'negative' : 'primary';
}

function lineClass(line: string) {
  if (line.startsWith('+')) return 'line-add';
  if (line.startsWith('-')) return 'line-delete';
  return 'line-context';
}
</script>
