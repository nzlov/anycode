import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('workflow result review renders the canonical approval projection', () => {
  const source = readFileSync(
    new URL('../src/components/WorkflowResultReview.vue', import.meta.url),
    'utf8',
  );

  assert.match(source, /normalizedResult\.summary/);
  assert.match(source, /关键结果/);
  assert.match(source, /验证结果/);
  assert.match(source, /系统验证/);
  assert.match(source, /Agent 验证/);
  assert.match(source, /注意事项/);
  assert.match(source, /原始结果/);
  assert.match(source, /flattenWorkflowResultData/);
  assert.match(source, /dataProjection\.truncated/);
  assert.match(source, /safeJSONStringify/);
  assert.match(source, /entry\.key}:\$\{index}/);
  assert.match(source, /check\.id}:\$\{index}/);
  assert.match(source, /warning\.code}:\$\{index}/);
  assert.match(source, /artifact\.ref}:\$\{index}/);
  assert.doesNotMatch(source, /function flattenData/);
  assert.match(source, /phase === 'after_run' && !normalizedResult/);
  assert.match(source, /结果恢复前不能提交审批/);
  assert.match(source, /phase === 'before_run'/);
  assert.match(source, /normalizeWorkflowNodeResult\(props\.result\)/);
  assert.doesNotMatch(source, /result\.checks\.length/);
});
