<template>
  <q-dialog
    :model-value="modelValue"
    persistent
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card class="questions-dialog app-content-dialog">
      <div class="questions-dialog__tabs">
        <q-tabs v-model="activeTab" dense align="left" class="text-primary">
          <q-tab name="questions" icon="quiz" label="问题" />
          <q-tab name="diff" icon="difference" label="Diff" />
        </q-tabs>
        <div class="questions-dialog__actions">
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
            flat
            round
            dense
            icon="close"
            aria-label="关闭"
            :disable="submitting"
            @click="emit('update:modelValue', false)"
          >
            <q-tooltip>关闭</q-tooltip>
          </q-btn>
        </div>
      </div>
      <q-separator />

      <q-tab-panels v-model="activeTab" animated class="questions-dialog__panels">
        <q-tab-panel name="questions" class="questions-dialog__panel questions-dialog__panel--questions">
          <QuestionsPanel
            class="questions-dialog__body"
            :requests="requests"
            :loading="loading"
            :submitting="submitting"
            show-close
            @close="emit('update:modelValue', false)"
            @submit="(requestId, answers) => emit('submit', requestId, answers)"
          />
        </q-tab-panel>
        <q-tab-panel name="diff" class="questions-dialog__panel">
          <DiffWorkspace v-if="modelValue" v-model="diffWorkspaceState" :target="diffTarget" />
        </q-tab-panel>
      </q-tab-panels>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue';
import type { RouteLocationRaw } from 'vue-router';

import QuestionsPanel from '@/components/QuestionsPanel.vue';
import DiffWorkspace from '@/components/DiffWorkspace.vue';

import type { DiffWorkspaceState, DiffWorkspaceTarget } from '@/services/diff';
import type { QuestionAnswerInput, QuestionRequest } from '@/services/sessions';

const props = defineProps<{
  modelValue: boolean;
  requests: QuestionRequest[];
  loading?: boolean;
  submitting?: boolean;
  diffTarget: DiffWorkspaceTarget;
  fullDiffRoute: RouteLocationRaw;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  submit: [requestId: string, answers: QuestionAnswerInput[]];
}>();

const activeTab = ref<'questions' | 'diff'>('questions');
const diffWorkspaceState = ref<DiffWorkspaceState>(initialDiffWorkspaceState());

function initialDiffWorkspaceState(): DiffWorkspaceState {
  return { mode: 'all', filePath: '' };
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
.questions-dialog {
  display: grid;
  grid-template-rows: auto auto minmax(0, 1fr);
  overflow: hidden;
}

.questions-dialog__tabs {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-right: 8px;
}

.questions-dialog__actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.questions-dialog__panels,
.questions-dialog__body {
  min-height: 0;
  overflow: hidden;
}

.questions-dialog__panel {
  height: 100%;
  overflow: auto;
}

.questions-dialog__panel--questions {
  padding: 0;
  overflow: hidden;
}

.questions-dialog__panel :deep(.diff-workspace) {
  height: 100%;
}

.questions-dialog__body {
  height: 100%;
}
</style>
