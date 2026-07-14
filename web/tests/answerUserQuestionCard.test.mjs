import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const panelSource = readFileSync(
  new URL('../src/components/AnswerUserPanel.vue', import.meta.url),
  'utf8',
);

test('question title and optional body share one question card', () => {
  const questionCard = panelSource.match(
    /<q-card[^>]*class="question-card"[^>]*>[\s\S]*?<\/q-card>/,
  )?.[0];

  assert.ok(questionCard, 'question card should exist');
  assert.match(
    questionCard,
    /class="question-title"[^>]*>\{\{ question\.title \|\| '未命名问题' \}\}/,
  );
  assert.match(
    questionCard,
    /v-if="question\.body" class="question-body">\{\{ question\.body \}\}/,
  );
  assert.equal((panelSource.match(/\{\{ question\.body \}\}/g) ?? []).length, 1);
});

test('legacy duplicated context block is removed', () => {
  assert.doesNotMatch(panelSource, /上下文/);
  assert.doesNotMatch(panelSource, /question-context/);
});

test('question card preserves multiline body text and wraps long content', () => {
  const questionCardStyles = panelSource.match(/\.question-card\s*\{[\s\S]*?\}/)?.[0];
  const questionBodyStyles = panelSource.match(/\.question-body\s*\{[\s\S]*?\}/)?.[0];

  assert.ok(questionCardStyles, 'question card styles should exist');
  assert.match(questionCardStyles, /overflow-wrap:\s*anywhere/);
  assert.ok(questionBodyStyles, 'question body styles should exist');
  assert.match(questionBodyStyles, /white-space:\s*pre-wrap/);
});
