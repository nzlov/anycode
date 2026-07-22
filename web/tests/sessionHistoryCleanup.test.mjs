import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('session history filters by age and confirms filtered cleanup', () => {
  const page = readSource('../src/pages/SessionsPage.vue');
  const composable = readSource('../src/composables/useSessionsPage.ts');
  const service = readSource('../src/services/sessions.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(page, /label: '3 天前', value: 3/);
  assert.match(page, /label: '7 天前', value: 7/);
  assert.match(page, /label: '30 天前', value: 30/);
  assert.match(page, /status\.value === 'closed'/);
  assert.match(page, /永久清理当前筛选出的 \$\{count\} 个已关闭会话及关联的 Codex 会话信息/);
  assert.match(page, /await cleanupSessions\(input\)/);
  assert.match(
    composable,
    /if \(olderThanDays\.value\) value\.olderThanDays = olderThanDays\.value/,
  );
  assert.match(service, /mutation CleanupSessions\(\$input: CleanupSessionsInput!\)/);
  assert.match(schema, /cleanupSessions\(input: CleanupSessionsInput!\): Int!/);
  assert.match(schema, /olderThanDays: Int/);
});
