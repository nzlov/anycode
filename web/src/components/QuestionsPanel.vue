<template>
  <div class="questions-panel">
    <q-inner-loading :showing="loading">
      <q-spinner color="primary" size="32px" />
    </q-inner-loading>

    <q-card-section v-if="questions.length === 0" class="empty-state">
      <q-icon name="task_alt" size="32px" color="positive" />
      <div class="text-body2">当前没有待回答问题</div>
    </q-card-section>

    <template v-else>
      <template v-if="questions.length > 1">
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
      </template>

      <q-tab-panels v-model="activeQuestionId" animated>
        <q-tab-panel v-for="question in questions" :key="question.id" :name="question.id">
          <q-card flat bordered class="question-card">
            <div class="question-body">{{ question.body }}</div>
          </q-card>

          <div class="text-body2 text-weight-bold q-mb-xs">选择答案</div>
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

            <q-item class="option-item option-item--custom">
              <q-item-section avatar>
                <q-radio
                  :model-value="drafts[question.id]?.choice"
                  val="__custom__"
                  @update:model-value="setChoice(question.id, String($event))"
                />
              </q-item-section>
              <q-item-section>
                <q-input
                  :model-value="draftFor(question.id).customAnswer"
                  dense
                  outlined
                  autogrow
                  class="custom-answer-input"
                  placeholder="输入自定义答案"
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
    <q-card-actions class="questions-panel__actions">
      <q-btn
        unelevated
        color="primary"
        class="app-on-primary"
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

import type { AgentQuestion, QuestionAnswerInput, QuestionRequest } from '@/services/sessions';

const props = defineProps<{
  requests: QuestionRequest[];
  loading?: boolean;
  submitting?: boolean;
}>();

const emit = defineEmits<{
  submit: [requestId: string, answers: QuestionAnswerInput[]];
}>();

interface DraftAnswer {
  choice: string;
  customAnswer: string;
}

const activeQuestionId = ref('');
const drafts = ref<Record<string, DraftAnswer>>({});
const currentRequest = computed(
  () => props.requests.find((request) => request.status === 'pending') ?? null,
);
const questions = computed<AgentQuestion[]>(() => currentRequest.value?.questions ?? []);
const canSubmit = computed(() => questions.value.every(hasValidDraft));

watch(
  () => props.requests,
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

  if (choice === '__custom__') return;
  const questionIndex = questions.value.findIndex((question) => question.id === questionId);
  const currentQuestion = questions.value[questionIndex];
  if (!currentQuestion || !hasValidDraft(currentQuestion)) return;
  const nextQuestion = questions.value
    .slice(questionIndex + 1)
    .find((question) => !hasValidDraft(question));
  if (nextQuestion) activeQuestionId.value = nextQuestion.id;
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

function hasValidDraft(question: AgentQuestion): boolean {
  const draft = drafts.value[question.id];
  if (!draft?.choice) return false;
  if (draft.choice === '__custom__') return draft.customAnswer.trim().length > 0;
  return question.options.some((option) => option.id === draft.choice);
}

function submit() {
  const request = currentRequest.value;
  if (!request || !canSubmit.value) return;
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
  emit('submit', request.id, answers);
}
</script>

<style scoped>
.questions-panel {
  position: relative;
  display: flex;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.questions-panel > .q-tab-panels {
  min-height: 0;
  flex: 1 1 auto;
  overflow-y: auto;
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
  margin: 8px 16px 0;
  padding: 4px;
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
  padding: 16px;
}

.question-card {
  display: grid;
  gap: 8px;
  margin-bottom: 10px;
  padding: 12px;
  border-color: var(--ac-status-warning-border);
  background: var(--ac-status-warning-bg);
  overflow-wrap: anywhere;
}

.question-body {
  color: var(--ac-status-warning-text);
  font-size: 16px;
  font-weight: 700;
  white-space: pre-wrap;
}

.option-list {
  display: grid;
  gap: 6px;
}

.option-item {
  border: 1px solid var(--ac-border);
  border-radius: var(--ac-radius);
  background: var(--ac-surface);
}

.custom-answer-input {
  margin: 0;
}

.questions-panel__actions {
  justify-content: flex-end;
  padding: 8px 16px 10px;
}

@media (max-width: 599.98px) {
  .question-tabs {
    margin: 8px 12px 0;
  }

  .q-tab-panel {
    padding: 12px;
  }

  .questions-panel__actions {
    padding: 8px 12px 10px;
  }
}
</style>
