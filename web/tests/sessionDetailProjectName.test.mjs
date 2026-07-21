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
