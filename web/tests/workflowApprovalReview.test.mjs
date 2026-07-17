import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  flattenWorkflowResultData,
  isPendingApprovalReviewable,
  normalizeWorkflowNodeResult,
} from '../src/services/workflowApprovalReview.ts';

const result = {
  version: 1,
  outcome: 'success',
  summary: 'done',
  data: {},
  checks: [],
  warnings: [],
  artifacts: [],
};

function approval(phase, approvalResult) {
  return {
    sessionId: 'session-1',
    nodeId: 'node-1',
    nodeRunId: 'node-run-1',
    currentNodeTitle: 'Review',
    phase,
    result: approvalResult,
  };
}

test('before-run approval is reviewable without an execution result', () => {
  assert.equal(isPendingApprovalReviewable(approval('before_run', null)), true);
});

test('after-run approval requires its persisted execution result', () => {
  assert.equal(isPendingApprovalReviewable(approval('after_run', result)), true);
  assert.equal(isPendingApprovalReviewable(approval('after_run', null)), false);
  assert.equal(isPendingApprovalReviewable(approval('after_run', {})), false);
  assert.equal(isPendingApprovalReviewable(approval('unknown', result)), false);
  assert.equal(isPendingApprovalReviewable(null), false);
});

test('workflow result normalization accepts only complete NodeResult v1 values', () => {
  assert.deepEqual(normalizeWorkflowNodeResult(result), result);
  assert.equal(normalizeWorkflowNodeResult({}), null);
  assert.equal(normalizeWorkflowNodeResult({ ...result, version: 2 }), null);
  assert.equal(normalizeWorkflowNodeResult({ ...result, checks: undefined }), null);
  assert.equal(normalizeWorkflowNodeResult({ ...result, warnings: [{}] }), null);
  assert.equal(normalizeWorkflowNodeResult({ ...result, artifacts: [{ kind: 'file' }] }), null);
});

test('workflow result data flattening is iterative and bounded', () => {
  const deep = {};
  let cursor = deep;
  for (let depth = 0; depth < 10_000; depth += 1) {
    cursor.next = {};
    cursor = cursor.next;
  }
  const deepProjection = flattenWorkflowResultData(deep, 8, 100);
  assert.equal(deepProjection.truncated, true);
  assert.equal(deepProjection.entries.length, 1);
  assert.match(String(deepProjection.entries[0].value), /层级过深/);

  const wide = Object.fromEntries(Array.from({ length: 150 }, (_, index) => [`key${index}`, index]));
  const wideProjection = flattenWorkflowResultData(wide, 8, 100);
  assert.equal(wideProjection.truncated, true);
  assert.equal(wideProjection.entries.length, 100);
});

test('both approval entry points reject duplicate in-flight submissions', () => {
  const overview = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');
  const detail = readFileSync(new URL('../src/composables/useSessionDetail.ts', import.meta.url), 'utf8');
  assert.match(overview, /async function submitApproval[\s\S]*?if \(approvalSubmitting\.value\) return;/);
  assert.match(detail, /async function submitApproval[\s\S]*?if \(approvalSubmitting\.value\) return;/);
});
