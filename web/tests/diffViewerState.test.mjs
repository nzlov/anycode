import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  DEFAULT_DIFF_CONTEXT,
  DIFF_EXPAND_STEP,
  collapseCurrentDiffPage,
  expandDiffContext,
  expandCurrentDiffPage,
  initialDiffCollapseState,
  initialDiffContext,
  isDiffFileCollapsed,
  syncDiffCollapseTarget,
  toggleDiffFileCollapsed,
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

test('diff files start expanded and single-file mode never collapses content', () => {
  const state = initialDiffCollapseState('session:one');

  assert.deepEqual(state, { targetKey: 'session:one', collapsedPaths: [] });
  assert.equal(isDiffFileCollapsed(state, 'all', 'a.txt'), false);
  assert.equal(isDiffFileCollapsed(state, 'single', 'a.txt'), false);
  assert.deepEqual(toggleDiffFileCollapsed(state, 'single', 'a.txt'), state);
});

test('diff files toggle independently and current-page controls only affect loaded paths', () => {
  let state = initialDiffCollapseState('session:one');
  state = toggleDiffFileCollapsed(state, 'all', 'a.txt');
  assert.equal(isDiffFileCollapsed(state, 'all', 'a.txt'), true);
  assert.equal(isDiffFileCollapsed(state, 'all', 'b.txt'), false);

  state = collapseCurrentDiffPage(state, 'all', ['b.txt', 'c.txt']);
  assert.deepEqual(state.collapsedPaths, ['a.txt', 'b.txt', 'c.txt']);

  state = expandCurrentDiffPage(state, 'all', ['b.txt', 'c.txt']);
  assert.deepEqual(state.collapsedPaths, ['a.txt']);

  state = toggleDiffFileCollapsed(state, 'all', 'a.txt');
  assert.deepEqual(state.collapsedPaths, []);
});

test('diff collapse state survives refreshes for one target and resets for a new target', () => {
  const collapsed = toggleDiffFileCollapsed(
    initialDiffCollapseState('session:one'),
    'all',
    'a.txt',
  );

  assert.deepEqual(syncDiffCollapseTarget(collapsed, 'session:one'), collapsed);
  assert.deepEqual(syncDiffCollapseTarget(collapsed, 'session:two'), {
    targetKey: 'session:two',
    collapsedPaths: [],
  });
});
