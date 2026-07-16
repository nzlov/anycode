import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const panelSource = readFileSync(
  new URL('../src/components/AnswerUserPanel.vue', import.meta.url),
  'utf8',
);
const dialogSource = readFileSync(
  new URL('../src/components/AnswerUserDialog.vue', import.meta.url),
  'utf8',
);
const detailSource = readFileSync(
  new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
  'utf8',
);

test('custom answer input is not nested in an actionable item or label', () => {
  const customItem = panelSource.match(
    /<q-item\s+class="option-item option-item--custom"[\s\S]*?<\/q-item>/,
  )?.[0];

  assert.ok(customItem, 'custom answer item should exist');
  assert.doesNotMatch(panelSource, /question\.allowCustom/);
  const openingTag = customItem.match(/^<q-item[^>]*>/)?.[0] ?? '';
  assert.doesNotMatch(openingTag, /\btag="label"/);
  assert.doesNotMatch(openingTag, /\bclickable\b/);
  assert.match(customItem, /<q-radio[\s\S]*?label="自定义答案"/);
  assert.match(customItem, /<q-input/);
});

test('dialog and session detail entries share the focus-safe answer panel', () => {
  assert.match(dialogSource, /<AnswerUserPanel/);
  assert.match(detailSource, /<AnswerUserPanel/);
});
