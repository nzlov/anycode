import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('web client exposes one executeSession command', () => {
  const serviceSource = readSource('../src/services/sessions.ts');

  assert.match(serviceSource, /export async function executeSession/);
  assert.match(serviceSource, /mutation ExecuteSession/);
  assert.doesNotMatch(serviceSource, /export async function startSession/);
  assert.doesNotMatch(serviceSource, /export async function resumeSession/);
});

test('overview and detail delegate execution choice to the backend', () => {
  const overviewSource = readSource('../src/pages/IndexPage.vue');
  const detailSource = readSource('../src/pages/SessionDetailPage.vue');
  const composableSource = readSource('../src/composables/useSessionDetail.ts');

  assert.match(overviewSource, /card\.availableActions\.includes\('execute'\)/);
  assert.match(overviewSource, /await executeSession\(card\.id, card\.status === 'queued'\)/);
  assert.doesNotMatch(overviewSource, /preferredSessionExecutionAction/);

  assert.match(detailSource, /const canExecute = computed/);
  assert.match(detailSource, /await executeSession\(\)/);
  assert.doesNotMatch(detailSource, /preferredSessionExecutionAction/);

  assert.match(composableSource, /executeSession as executeSessionRequest/);
  assert.doesNotMatch(composableSource, /startSession as startSessionRequest/);
  assert.doesNotMatch(composableSource, /resumeSession as resumeSessionRequest/);
});

test('session status presentation has one owner', () => {
  const overviewSource = readSource('../src/pages/IndexPage.vue');
  const detailSource = readSource('../src/pages/SessionDetailPage.vue');
  const tableSource = readSource('../src/pages/SessionsPage.vue');
  const serviceSource = readSource('../src/services/sessions.ts');

  for (const source of [overviewSource, detailSource, tableSource, serviceSource]) {
    assert.match(source, /sessionStatus(?:Label|Presentation)/);
  }
  assert.doesNotMatch(overviewSource, /created: '待运行'/);
  assert.doesNotMatch(detailSource, /created: '待运行'/);
  assert.doesNotMatch(tableSource, /created: '待运行'/);
});
