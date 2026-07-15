<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.lt.sm"
    persistent
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card class="answer-dialog app-content-dialog">
      <div class="answer-dialog__tabs">
        <q-tabs v-model="activeTab" dense align="left" class="text-primary">
          <q-tab name="questions" icon="quiz" label="问题" />
          <q-tab name="diff" icon="difference" label="Diff" />
        </q-tabs>
        <div class="answer-dialog__actions">
          <q-btn
            flat
            round
            dense
            icon="open_in_new"
            aria-label="打开完整 Diff 页面"
            :to="fullDiffRoute"
          >
            <q-tooltip>打开完整 Diff 页面</q-tooltip>
          </q-btn>
          <q-btn
            v-close-popup
            flat
            round
            dense
            icon="close"
            aria-label="关闭"
            :disable="submitting"
          >
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </div>
      </div>
      <q-separator />

      <q-tab-panels v-model="activeTab" animated class="answer-dialog__panels">
        <q-tab-panel name="questions" class="answer-dialog__panel answer-dialog__panel--questions">
          <AnswerUserPanel
            class="answer-dialog__body"
            :batches="batches"
            :loading="loading"
            :submitting="submitting"
            show-close
            @close="emit('update:modelValue', false)"
            @submit="(batchId, answers) => emit('submit', batchId, answers)"
          />
        </q-tab-panel>
        <q-tab-panel name="diff" class="answer-dialog__panel">
          <DiffWorkspace v-if="modelValue" v-model="diffWorkspaceState" :target="diffTarget" />
        </q-tab-panel>
      </q-tab-panels>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import type { RouteLocationRaw } from 'vue-router';

import AnswerUserPanel from '@/components/AnswerUserPanel.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';

import type { DiffWorkspaceState, DiffWorkspaceTarget } from '@/services/diff';
import type { QuestionAnswerInput, QuestionBatch } from '@/services/sessions';

const props = defineProps<{
  modelValue: boolean;
  batches: QuestionBatch[];
  loading?: boolean;
  submitting?: boolean;
  diffTarget: DiffWorkspaceTarget;
  fullDiffRoute: RouteLocationRaw;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  submit: [batchId: string, answers: QuestionAnswerInput[]];
}>();

const activeTab = ref<'questions' | 'diff'>('questions');
const diffWorkspaceState = ref<DiffWorkspaceState>(initialDiffWorkspaceState());

function initialDiffWorkspaceState(): DiffWorkspaceState {
  return { mode: 'all', filePath: '', page: 1, pageSize: 20 };
}

watch(
  () => props.modelValue,
  (modelValue) => {
    if (!modelValue) return;
    activeTab.value = 'questions';
    diffWorkspaceState.value = initialDiffWorkspaceState();
  },
);
</script>

<style scoped>
.answer-dialog {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  overflow: hidden;
}

.answer-dialog__tabs {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-right: 8px;
}

.answer-dialog__actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.answer-dialog__panels,
.answer-dialog__body {
  min-height: 0;
  overflow: hidden;
}

.answer-dialog__panel {
  height: 100%;
  overflow: auto;
}

.answer-dialog__panel--questions {
  padding: 0;
  overflow: hidden;
}

.answer-dialog__panel :deep(.diff-workspace) {
  height: 100%;
}

.answer-dialog__body {
  height: 100%;
}
</style>
