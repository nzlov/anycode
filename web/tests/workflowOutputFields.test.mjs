import assert from 'node:assert/strict';
import { test } from 'node:test';

import { completeOutputFields, systemOutputFields } from '../src/services/workflowOutputFields.js';

test('removes stale system approval output when approval is turned off', () => {
  const withApproval = completeOutputFields(
    [{ key: 'status', description: '节点执行结果', valueType: 'string' }],
    systemOutputFields('codex', true, false),
  );

  assert.deepEqual(
    withApproval.map((field) => field.key),
    ['status', 'approval.approved'],
  );

  const withoutApproval = completeOutputFields(withApproval, systemOutputFields('codex', false, false));

  assert.deepEqual(withoutApproval, [{ key: 'status', description: '节点执行结果', valueType: 'string' }]);
});

test('preserves custom fields while replacing active system fields', () => {
  const fields = completeOutputFields(
    [
      { key: 'result', description: '自定义结果', valueType: 'string' },
      { key: 'approval.approved', description: 'old label', valueType: 'string' },
    ],
    systemOutputFields('codex', true, false),
  );

  assert.deepEqual(fields, [
    { key: 'result', description: '自定义结果', valueType: 'string' },
    { key: 'approval.approved', description: '人工审批是否通过', valueType: 'boolean' },
  ]);
});
