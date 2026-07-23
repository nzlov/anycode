import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('session table renders the requirement once and opens details by row click', () => {
  const source = readSource('../src/pages/SessionsPage.vue');

  assert.match(source, /\{\{ props\.row\.title \}\}/);
  assert.doesNotMatch(source, /props\.row\.summary/);
  assert.match(source, /@row-click="openSession"/);
  assert.match(source, /router\.push\(`\/sessions\/\$\{session\.id\}`\)/);
  assert.match(source, /<router-link[\s\S]*?@click\.stop/);
  assert.doesNotMatch(source, /打开卡片详情/);
});

test('session table defaults to 15 rows and offers the requested page sizes', () => {
  const source = readSource('../src/pages/SessionsPage.vue');

  assert.match(source, /:rows-per-page-options="\[15, 25, 50\]"/);
  assert.match(source, /useSessionsPage\(\{[\s\S]*?pageSize: 15,/);
});

test('session card summary remains available without being rendered in the table', () => {
  const source = readSource('../src/services/sessions.ts');
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

  assert.match(source, /summary: string;/);
  assert.match(source, /requirementSummary/);
  assert.match(schema, /requirementSummary: String!/);
});

test('session table renders current nodes only for workflow rows and renders persisted statistics', () => {
  const source = readSource('../src/pages/SessionsPage.vue');

  assert.match(
    source,
    /#body-cell-node[\s\S]*?<span v-if="props\.row\.mode === 'workflow'">[\s\S]*?props\.row\.node/,
  );
  assert.doesNotMatch(source, /props\.row\.mode === 'workflow' \? props\.row\.node : '-'/);
  assert.match(source, /name: 'diff',[\s\S]*?row\.filesChanged/);
  assert.match(source, /name: 'tokens',[\s\S]*?row\.usage\?\.totalTokens \?\? 0/);
  assert.match(source, /#body-cell-diff[\s\S]*?props\.row\.filesChanged/);
  assert.match(
    source,
    /#body-cell-tokens[\s\S]*?<TokenUsageDisplay v-if="props\.row\.usage" :usage="props\.row\.usage"/,
  );
  assert.match(source, /\['title', 'diff', 'tokens', 'updatedAt', 'status', 'actions'\]/);
});

test('session table and overview cards share the token usage presentation', () => {
  const table = readSource('../src/pages/SessionsPage.vue');
  const overview = readSource('../src/pages/IndexPage.vue');
  const display = readSource('../src/components/TokenUsageDisplay.vue');

  assert.match(table, /import TokenUsageDisplay/);
  assert.match(overview, /<TokenUsageDisplay :usage="card\.usage"/);
  assert.match(display, /formatTokenCount\(usage\.totalTokens\)/);
  assert.match(
    display,
    /输入 Token[\s\S]*?Math\.max\(usage\.inputTokens - usage\.cachedInputTokens, 0\)[\s\S]*?输出 Token[\s\S]*?usage\.outputTokens[\s\S]*?缓存 Token[\s\S]*?usage\.cachedInputTokens/,
  );
});
