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

test('mobile session history uses a dialog that restores the current filters', () => {
  const page = readSource('../src/pages/SessionsPage.vue');
  const styles = readSource('../src/css/app.scss');

  assert.match(page, /v-if="!\$q\.screen\.lt\.sm" class="sessions-toolbar"/);
  assert.match(
    page,
    /v-else class="sessions-toolbar sessions-toolbar--mobile"[\s\S]*aria-label="设置会话搜索条件"[\s\S]*@click="openMobileFilters"/,
  );
  assert.match(page, /<q-dialog v-model="mobileFilterDialogOpen">/);
  assert.match(
    page,
    /function openMobileFilters\(\) \{[\s\S]*mobileFilterDraft\.value = filter\.value;[\s\S]*mobileStatusDraft\.value = status\.value;[\s\S]*mobileAgeDraft\.value = olderThanDays\.value;/,
  );
  assert.match(
    page,
    /function applyMobileFilters\(\) \{[\s\S]*filter\.value = mobileFilterDraft\.value;[\s\S]*status\.value = mobileStatusDraft\.value;[\s\S]*olderThanDays\.value = mobileAgeDraft\.value;/,
  );
  assert.match(styles, /\.sessions-toolbar--mobile\s*{[^}]*overflow:\s*visible/s);
  assert.match(styles, /\.sessions-filter-dialog\s*{[^}]*max-width:\s*calc\(100vw - 24px\)/s);
});
