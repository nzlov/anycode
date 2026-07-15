import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { completeOutputFields, systemOutputFields } from '../src/services/workflowOutputFields.js';

test('keeps workflow approval outside node output fields', () => {
  const withApproval = completeOutputFields(
    [{ key: 'status', description: '节点执行结果', valueType: 'string' }],
    systemOutputFields('codex', false),
  );

  assert.deepEqual(
    withApproval.map((field) => field.key),
    ['status'],
  );

  const withoutApproval = completeOutputFields(withApproval, systemOutputFields('codex', false));

  assert.deepEqual(withoutApproval, [{ key: 'status', description: '节点执行结果', valueType: 'string' }]);
});

test('preserves custom fields while replacing active system fields', () => {
  const fields = completeOutputFields(
    [
      { key: 'result', description: '自定义结果', valueType: 'string' },
      { key: 'approval.approved', description: 'old label', valueType: 'string' },
    ],
    systemOutputFields('codex', false),
  );

  assert.deepEqual(fields, [
    { key: 'result', description: '自定义结果', valueType: 'string' },
  ]);
});

test('workflow config persists after-run forward approval', () => {
  const source = readFileSync(new URL('../src/pages/WorkflowConfigPage.vue', import.meta.url), 'utf8');

  assert.match(source, /v-model="requiresForwardApproval"/);
  assert.match(source, /label="运行后前进审核"/);
  assert.match(source, /node\.approval\.afterRun = approvalAfterRun/);
  assert.doesNotMatch(source, /node\.approval\.afterRun = false/);
});
