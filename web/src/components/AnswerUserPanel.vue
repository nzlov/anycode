<template>
  <div class="answer-panel">
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
        align="left"
        active-class="question-tab--active"
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

          <q-card v-if="question.body" flat bordered class="question-context">
            <div class="text-caption text-negative text-weight-bold">上下文</div>
            <div class="mono question-context__text">{{ question.body }}</div>
          </q-card>

          <div class="text-body2 text-weight-bold q-mb-sm">选择答案</div>
          <q-list class="option-list">
            <q-item
              v-for="option in question.options"
              :key="option.id"
              tag="label"
              clickable
              class="option-item"
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

            <q-item v-if="question.allowCustom" class="option-item option-item--custom">
              <q-item-section>
                <q-radio
                  :model-value="drafts[question.id]?.choice"
                  val="__custom__"
                  label="自定义答案"
                  @update:model-value="setChoice(question.id, String($event))"
                />
                <q-input
                  :model-value="draftFor(question.id).customAnswer"
                  dense
                  outlined
                  autogrow
                  class="custom-answer-input"
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
    <q-card-actions class="answer-panel__actions">
      <q-btn
        v-if="showClose"
        flat
        round
        icon="close"
        aria-label="取消"
        :disable="submitting"
        @click="emit('close')"
      >
        <q-tooltip>取消</q-tooltip>
      </q-btn>
      <q-space v-else />
      <q-btn
        unelevated
        color="positive"
        text-color="dark"
        icon="send"
        label="提交全部答案并继续"
        no-caps
        :loading="submitting"
        :disable="questions.length === 0 || !canSubmit"
        @click="submit"
      />
    </q-card-actions>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';

import type { AgentQuestion, QuestionAnswerInput, QuestionBatch } from '@/services/sessions';

const props = defineProps<{
  batches: QuestionBatch[];
  loading?: boolean;
  submitting?: boolean;
  showClose?: boolean;
}>();

const emit = defineEmits<{
  close: [];
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
.answer-panel {
  position: relative;
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
  margin: 14px 36px 0;
  padding: 8px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface-muted);
  color: var(--ac-text-muted);
}

.question-tabs :deep(.q-tab) {
  min-height: 32px;
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.question-tabs :deep(.question-tab--active) {
  background: var(--ac-text);
  color: var(--ac-surface);
}

.question-tabs :deep(.q-tab__indicator) {
  display: none;
}

.q-tab-panel {
  padding: 36px;
}

.question-title {
  margin-bottom: 6px;
  color: var(--ac-text);
  font-size: 18px;
  font-weight: 800;
}

.question-body {
  margin-bottom: 14px;
  color: var(--ac-text-muted);
  white-space: pre-wrap;
}

.question-context {
  display: grid;
  gap: 8px;
  margin-bottom: 22px;
  padding: 18px;
  border-color: #fed7aa;
  background: #fff7ed;
}

.question-context__text {
  color: #9a3412;
  font-size: 13px;
  white-space: pre-wrap;
}

.option-list {
  display: grid;
  gap: 10px;
}

.option-item {
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.option-item--custom {
  border-color: #67e8f9;
  background: #ecfeff;
}

.custom-answer-input {
  margin: 8px 0 0 40px;
}

.answer-panel__actions {
  justify-content: space-between;
  padding: 12px 36px 22px;
}

@media (max-width: 699px) {
  .question-tabs {
    margin: 12px 16px 0;
  }

  .q-tab-panel {
    padding: 20px 16px;
  }

  .answer-panel__actions {
    padding: 12px 16px 18px;
  }
}
</style>
