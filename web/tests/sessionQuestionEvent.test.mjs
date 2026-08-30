import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(path) {
  return readFileSync(new URL(path, import.meta.url), 'utf8');
}

const toolEventSource = readSource('../src/components/SessionToolEvent.vue');
const questionsPanelSource = readSource('../src/components/QuestionsPanel.vue');
const questionsDialogSource = readSource('../src/components/QuestionsDialog.vue');
const sessionDetailSource = readSource('../src/components/SessionDetailView.vue');
const sessionsServiceSource = readSource('../src/services/sessions.ts');

test('question tool events load the persisted request instead of rendering raw input and output', () => {
  assert.match(toolEventSource, /getQuestionRequest\(requestId\)/);
  assert.match(toolEventSource, /JSON\.parse\(output\)[\s\S]*requestId/);
  assert.match(toolEventSource, /<template v-if="isQuestionEvent">[\s\S]*<QuestionsPanel/);
  assert.match(
    toolEventSource,
    /<section v-else-if="content\.input\.text"[\s\S]*<StructuredContent :content="content\.input"/,
  );
  assert.match(
    toolEventSource,
    /<section v-if="!isQuestionEvent && content\.output\.text"[\s\S]*<StructuredContent :content="content\.output"/,
  );
});

test('question request query reads the authoritative stored questions and answers', () => {
  assert.match(
    sessionsServiceSource,
    /export async function getQuestionRequest\(requestId: string\): Promise<QuestionRequest>/,
  );
  assert.match(sessionsServiceSource, /questionRequest\(id: \$requestId\)/);
  assert.match(sessionsServiceSource, /return normalizeQuestionRequest\(data\.questionRequest\)/);
});

test('questions panel is read-only by default and restores persisted answers', () => {
  assert.match(questionsPanelSource, /\{ readonly: true \}/);
  assert.match(
    questionsPanelSource,
    /choice: question\.selectedOptionId \|\| \(question\.customAnswer \? '__custom__' : ''\)/,
  );
  assert.match(questionsPanelSource, /\{\{ readonly \? '用户回答' : '选择答案' \}\}/);
  assert.match(questionsPanelSource, /<q-card-actions v-if="!readonly"/);
  assert.match(questionsDialogSource, /<QuestionsPanel[\s\S]*?:readonly="false"/);
  assert.match(sessionDetailSource, /<QuestionsPanel[\s\S]*?:readonly="false"/);
});
