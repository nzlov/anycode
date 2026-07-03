<template>
  <q-dialog :model-value="modelValue" persistent @update:model-value="emit('update:modelValue', $event)">
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

      <q-inner-loading :showing="loading">
        <q-spinner color="primary" size="32px" />
      </q-inner-loading>

      <q-card-section v-if="questions.length === 0" class="empty-state">
        <q-icon name="task_alt" size="32px" color="positive" />
        <div class="text-body2">当前没有待回答问题</div>
      </q-card-section>

      <template v-else>
        <q-tabs
          v-model="activeQuestionId"
          dense
          outside-arrows
          mobile-arrows
          class="question-tabs"
        >
          <q-tab
            v-for="(question, index) in questions"
            :key="question.id"
            :name="question.id"
            :label="`问题 ${index + 1}`"
          />
        </q-tabs>
        <q-separator />

        <q-tab-panels v-model="activeQuestionId" animated>
          <q-tab-panel v-for="question in questions" :key="question.id" :name="question.id">
            <div class="question-title">{{ question.title || '未命名问题' }}</div>
            <div v-if="question.body" class="question-body">{{ question.body }}</div>

            <q-list bordered separator class="option-list">
              <q-item
                v-for="option in question.options"
                :key="option.id"
                tag="label"
                clickable
              >
                <q-item-section avatar>
                  <q-radio
                    :model-value="drafts[question.id]?.choice"
                    :val="option.id"
                    @update:model-value="setChoice(question.id, String($event))"
                  />
                </q-item-section>
                <q-item-section>
                  <q-item-label>{{ option.label }}</q-item-label>
                  <q-item-label v-if="option.description" caption>
                    {{ option.description }}
                  </q-item-label>
                </q-item-section>
              </q-item>

              <q-item v-if="question.allowCustom" tag="label" clickable>
                <q-item-section avatar>
                  <q-radio
                    :model-value="drafts[question.id]?.choice"
                    val="__custom__"
                    @update:model-value="setChoice(question.id, String($event))"
                  />
                </q-item-section>
                <q-item-section>
                  <q-item-label>自定义答案</q-item-label>
                  <q-input
                    :model-value="draftFor(question.id).customAnswer"
                    dense
                    outlined
                    autogrow
                    class="q-mt-sm"
                    placeholder="输入自己的答案"
                    :disable="drafts[question.id]?.choice !== '__custom__'"
                    @update:model-value="setCustomAnswer(question.id, String($event ?? ''))"
                  />
                </q-item-section>
              </q-item>
            </q-list>
          </q-tab-panel>
        </q-tab-panels>
      </template>

      <q-separator />
      <q-card-actions align="right">
        <q-btn flat label="取消" no-caps :disable="submitting" v-close-popup />
        <q-btn
          unelevated
          color="primary"
          icon="send"
          label="提交回答"
          no-caps
          :loading="submitting"
          :disable="questions.length === 0 || !canSubmit"
          @click="submit"
        />
      </q-card-actions>
    </q-card>
  </q-dialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import type { AgentQuestion, QuestionAnswerInput, QuestionBatch } from '@/services/sessions';

const props = defineProps<{
  modelValue: boolean;
  batches: QuestionBatch[];
  loading?: boolean;
  submitting?: boolean;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  submit: [batchId: string, answers: QuestionAnswerInput[]];
}>();

interface DraftAnswer {
  choice: string;
  customAnswer: string;
}

const activeQuestionId = ref('');
const drafts = ref<Record<string, DraftAnswer>>({});
const currentBatch = computed(() => props.batches.find((batch) => batch.status === 'pending') ?? null);
const questions = computed<AgentQuestion[]>(() => currentBatch.value?.questions ?? []);
const canSubmit = computed(() =>
  questions.value.every((question) => {
    const draft = drafts.value[question.id];
    if (!draft?.choice) return false;
    if (draft.choice === '__custom__') return draft.customAnswer.trim().length > 0;
    return question.options.some((option) => option.id === draft.choice);
  }),
);

watch(
  () => props.batches,
  () => {
    const nextDrafts: Record<string, DraftAnswer> = {};
    for (const question of questions.value) {
      nextDrafts[question.id] = drafts.value[question.id] ?? { choice: '', customAnswer: '' };
    }
    drafts.value = nextDrafts;
    activeQuestionId.value = questions.value[0]?.id ?? '';
  },
  { immediate: true, deep: true },
);

function setChoice(questionId: string, choice: string) {
  drafts.value[questionId] = {
    ...draftFor(questionId),
    choice,
  };
}

function setCustomAnswer(questionId: string, customAnswer: string) {
  drafts.value[questionId] = {
    ...draftFor(questionId),
    customAnswer,
  };
}

function draftFor(questionId: string): DraftAnswer {
  return drafts.value[questionId] ?? { choice: '', customAnswer: '' };
}

function submit() {
  const batch = currentBatch.value;
  if (!batch || !canSubmit.value) return;
  const answers = questions.value.map((question) => {
    const draft = draftFor(question.id);
    if (draft.choice === '__custom__') {
      return {
        questionId: question.id,
        customAnswer: draft.customAnswer.trim(),
        payload: { custom: true },
      };
    }
    const option = question.options.find((item) => item.id === draft.choice);
    return {
      questionId: question.id,
      selectedOptionId: draft.choice,
      payload: option?.payload ?? {},
    };
  });
  emit('submit', batch.id, answers);
}
</script>

<style scoped>
.answer-dialog {
  width: min(720px, 92vw);
  max-width: 92vw;
}

.dialog-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.empty-state {
  display: grid;
  min-height: 180px;
  place-items: center;
  align-content: center;
  gap: 8px;
  color: var(--ac-text-muted);
}

.question-tabs {
  color: var(--ac-text-muted);
}

.question-title {
  margin-bottom: 6px;
  color: var(--ac-text);
  font-weight: 700;
}

.question-body {
  margin-bottom: 14px;
  color: var(--ac-text-muted);
  white-space: pre-wrap;
}

.option-list {
  border-color: var(--ac-border);
  border-radius: var(--ac-radius);
}

@media (max-width: 699px) {
  .answer-dialog {
    width: 100vw;
    max-width: 100vw;
  }
}
</style>
