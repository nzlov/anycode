import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const panelSource = readFileSync(
  new URL('../src/components/QuestionsPanel.vue', import.meta.url),
  'utf8',
);
const dialogSource = readFileSync(
  new URL('../src/components/QuestionsDialog.vue', import.meta.url),
  'utf8',
);
const detailSource = readFileSync(
  new URL('../src/components/SessionDetailView.vue', import.meta.url),
  'utf8',
);

test('custom answer matches option rows and requires explicit selection before input', () => {
  const customItem = panelSource.match(
    /<q-item\s+class="option-item option-item--custom"[\s\S]*?<\/q-item>/,
  )?.[0];
  const customInput = panelSource.match(
    /<q-input[\s\S]*?class="custom-answer-input"[\s\S]*?\/>/,
  )?.[0];

  assert.ok(customItem, 'custom answer option should exist');
  assert.ok(customInput, 'custom answer input should exist');
  assert.doesNotMatch(panelSource, /question\.allowCustom/);
  const openingTag = customItem.match(/^<q-item[^>]*>/)?.[0] ?? '';
  assert.doesNotMatch(openingTag, /\btag="label"|\bclickable\b/);
  assert.match(
    customItem,
    /<q-radio[\s\S]*?val="__custom__"[\s\S]*?@update:model-value="setChoice\(question\.id, String\(\$event\)\)"/,
  );
  assert.match(customInput, /placeholder="输入自定义答案"/);
  assert.match(customInput, /:disable="drafts\[question\.id\]\?\.choice !== '__custom__'"/);
  assert.doesNotMatch(customInput, /@focus=/);
  assert.match(
    customInput,
    /@update:model-value="setCustomAnswer\(question\.id, String\(\$event \?\? ''\)\)"/,
  );
});

test('single questions omit question tabs and their separator', () => {
  assert.match(
    panelSource,
    /<template v-if="questions\.length > 1">[\s\S]*?<q-tabs[\s\S]*?<q-separator \/>[\s\S]*?<\/template>/,
  );
});

test('dialog and session detail entries share the focus-safe questions panel', () => {
  assert.match(dialogSource, /<QuestionsPanel/);
  assert.match(detailSource, /<QuestionsPanel/);
});

test('questions dialog omits dismiss and full diff shortcuts', () => {
  assert.doesNotMatch(panelSource, /showClose|aria-label="取消"|emit\('close'\)/);
  assert.doesNotMatch(
    dialogSource,
    /questions-dialog__actions|打开完整 Diff 页面|aria-label="关闭"|fullDiffRoute|show-close/,
  );
});
