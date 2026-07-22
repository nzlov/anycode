import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('session detail displays the project name returned by its query', () => {
  const source = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');
  const detailFields = /const sessionDetailFields = `(?<fields>[\s\S]*?)`/.exec(source);
  const normalizer = /function normalizeSessionDetail[\s\S]*?\n}/.exec(source);

  assert.match(detailFields?.groups?.fields ?? '', /\bprojectName\b/);
  assert.match(normalizer?.[0] ?? '', /projectName: session\.projectName/);
  assert.doesNotMatch(normalizer?.[0] ?? '', /projectName: session\.projectId/);
});

test('session detail renders current node information only in workflow mode', () => {
  const source = readFileSync(new URL('../src/pages/SessionDetailPage.vue', import.meta.url), 'utf8');

  assert.match(
    source,
    /<q-item v-if="session\?\.mode === 'workflow'">\s*<q-item-section>\s*<q-item-label caption>当前节点<\/q-item-label>/,
  );
});
