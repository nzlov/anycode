<template>
  <q-dialog
    :model-value="modelValue"
    :maximized="$q.screen.width <= 699"
    persistent
    @update:model-value="emit('update:modelValue', $event)"
  >
    <q-card class="answer-dialog">
      <q-card-section class="dialog-header">
        <div>
          <div class="text-subtitle1 text-weight-bold">待回答问题</div>
          <div class="text-caption text-muted">选择每个问题的答案后一起提交。</div>
        </div>
        <q-btn flat round dense icon="close" :disable="submitting" v-close-popup>
          <q-tooltip>关闭</q-tooltip>
        </q-btn>
      </q-card-section>
      <q-separator />

      <AnswerUserPanel
        :batches="batches"
        :loading="loading"
        :submitting="submitting"
        show-close
        @close="emit('update:modelValue', false)"
        @submit="(batchId, answers) => emit('submit', batchId, answers)"
      />
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import AnswerUserPanel from '@/components/AnswerUserPanel.vue';

import type { QuestionAnswerInput, QuestionBatch } from '@/services/sessions';

defineProps<{
  modelValue: boolean;
  batches: QuestionBatch[];
  loading?: boolean;
  submitting?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  submit: [batchId: string, answers: QuestionAnswerInput[]];
}>();
</script>

<style scoped>
.answer-dialog {
  width: min(880px, 92vw);
  max-width: 92vw;
}

.dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

@media (max-width: 699px) {
  .answer-dialog {
    width: 100vw;
    max-width: 100vw;
    height: 100dvh;
    max-height: 100dvh;
    border-radius: 0;
  }
}
</style>
