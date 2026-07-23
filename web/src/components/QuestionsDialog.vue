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
      </div>
      <q-separator />

      <q-tab-panels v-model="activeTab" animated class="questions-dialog__panels">
        <q-tab-panel name="questions" class="questions-dialog__panel questions-dialog__panel--questions">
          <QuestionsPanel
            class="questions-dialog__body"
            :requests="requests"
            :loading="loading"
            :submitting="submitting"
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

.questions-dialog.app-content-dialog {
  max-height: none !important;
}

.questions-dialog__tabs {
  display: flex;
  align-items: center;
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
