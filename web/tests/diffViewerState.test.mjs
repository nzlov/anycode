import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  DEFAULT_DIFF_CONTEXT,
  DIFF_EXPAND_STEP,
  expandDiffContext,
  initialDiffContext,
} from '../src/services/diffViewerState.js';

test('initialDiffContext starts with ten lines on each side', () => {
  assert.equal(DEFAULT_DIFF_CONTEXT, 10);
  assert.equal(DIFF_EXPAND_STEP, 20);
  assert.deepEqual(initialDiffContext(), { before: 10, after: 10 });
});

test('expandDiffContext expands only the requested side by twenty lines', () => {
  assert.deepEqual(expandDiffContext({ before: 10, after: 10 }, 'before'), {
    before: 30,
    after: 10,
  });
  assert.deepEqual(expandDiffContext({ before: 30, after: 10 }, 'after'), {
    before: 30,
    after: 30,
  });
});
