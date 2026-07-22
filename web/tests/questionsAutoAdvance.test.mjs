import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const panelSource = readFileSync(
  new URL('../src/components/QuestionsPanel.vue', import.meta.url),
  'utf8',
);

test('answer validity is shared by submission and automatic navigation', () => {
  assert.match(
    panelSource,
    /const canSubmit = computed\(\(\) => questions\.value\.every\(hasValidDraft\)\);/,
  );
  assert.match(panelSource, /function hasValidDraft\(question: AgentQuestion\): boolean/);
  assert.match(
    panelSource,
    /draft\.choice === '__custom__'[\s\S]*draft\.customAnswer\.trim\(\)\.length > 0/,
  );
  assert.match(
    panelSource,
    /question\.options\.some\(\(option\) => option\.id === draft\.choice\)/,
  );
});

test('preset choices advance to the next unanswered question without wrapping or submitting', () => {
  const setChoice = panelSource.match(
    /function setChoice\(questionId: string, choice: string\) \{[\s\S]*?\n\}/,
  )?.[0];

  assert.ok(setChoice, 'setChoice should exist');
  assert.match(setChoice, /choice === '__custom__'/);
  assert.match(
    setChoice,
    /questions\.value\.findIndex\(\(question\) => question\.id === questionId\)/,
  );
  assert.match(setChoice, /questions\.value\s*\.slice\(questionIndex \+ 1\)/);
  assert.match(setChoice, /find\(\(question\) => !hasValidDraft\(question\)\)/);
  assert.match(setChoice, /activeQuestionId\.value = nextQuestion\.id/);
  assert.doesNotMatch(setChoice, /submit\(/);
});

test('custom answer selection keeps using setChoice without a separate advance handler', () => {
  const customItem = panelSource.match(
    /<q-item\s+class="option-item option-item--custom"[\s\S]*?<\/q-item>/,
  )?.[0];

  assert.ok(customItem, 'custom answer item should exist');
  assert.match(
    customItem,
    /val="__custom__"[\s\S]*@update:model-value="setChoice\(question\.id, String\(\$event\)\)"/,
  );
  assert.doesNotMatch(customItem, /advance/i);
});
