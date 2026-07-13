import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  activeOverviewDiffSessionIds,
  createOverviewDiffSummaryController,
} from '../src/services/overviewDiffSummary.js';

test('active diff polling only targets running lifecycle states', () => {
  const ids = activeOverviewDiffSessionIds([
    { id: 'starting', status: 'starting' },
    { id: 'running', status: 'running' },
    { id: 'waiting', status: 'waiting_user' },
    { id: 'stopping', status: 'stopping' },
    { id: 'approval', status: 'waiting_approval' },
    { id: 'closed', status: 'closed' },
  ]);

  assert.deepEqual(ids, ['starting', 'running', 'waiting', 'stopping']);
});

test('diff summary controller drains queued ids without overlapping requests', async () => {
  const releases = [];
  const calls = [];
  let inFlight = 0;
  let maxInFlight = 0;
  const applied = [];
  const controller = createOverviewDiffSummaryController({
    getVisibleCards: () => [],
    isPageVisible: () => true,
    loadSummaries: async (sessionIds) => {
      calls.push(sessionIds);
      inFlight += 1;
      maxInFlight = Math.max(maxInFlight, inFlight);
      await new Promise((resolve) => releases.push(resolve));
      inFlight -= 1;
      return sessionIds.map((sessionId) => ({ sessionId, state: 'clean', filesChanged: 0 }));
    },
    applySummaries: (sessionIds, summaries) => applied.push({ sessionIds, summaries }),
  });

  const first = controller.refresh(['session-1', 'session-1']);
  await Promise.resolve();
  const second = controller.refresh(['session-2']);
  assert.equal(calls.length, 1);
  assert.deepEqual(calls[0], ['session-1']);

  releases.shift()();
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(calls.length, 2);
  assert.deepEqual(calls[1], ['session-2']);
  releases.shift()();
  await Promise.all([first, second]);

  assert.equal(maxInFlight, 1);
  assert.equal(applied.length, 2);
});

test('diff summary controller keeps draining queued ids after a batch fails', async () => {
  let releaseFirst;
  const calls = [];
  const applied = [];
  const controller = createOverviewDiffSummaryController({
    getVisibleCards: () => [],
    isPageVisible: () => true,
    loadSummaries: async (sessionIds) => {
      calls.push(sessionIds);
      if (calls.length === 1) {
        await new Promise((resolve) => {
          releaseFirst = resolve;
        });
        throw new Error('batch failed');
      }
      return sessionIds.map((sessionId) => ({ sessionId, state: 'clean', filesChanged: 0 }));
    },
    applySummaries: (sessionIds, summaries) => applied.push({ sessionIds, summaries }),
  });

  const first = controller.refresh(['session-1']);
  await Promise.resolve();
  const second = controller.refresh(['session-2']);
  releaseFirst();
  const results = await Promise.allSettled([first, second]);

  assert.deepEqual(calls, [['session-1'], ['session-2']]);
  assert.deepEqual(
    applied.map((entry) => entry.sessionIds),
    [['session-2']],
  );
  assert.deepEqual(
    results.map((result) => result.status),
    ['rejected', 'rejected'],
  );
});

test('diff summary controller clears polling timers on stop', () => {
  const timers = new Map();
  let nextTimer = 1;
  const controller = createOverviewDiffSummaryController({
    getVisibleCards: () => [{ id: 'session-1', status: 'running' }],
    isPageVisible: () => true,
    loadSummaries: async () => [],
    applySummaries: () => undefined,
    setTimer: (callback) => {
      const id = nextTimer++;
      timers.set(id, callback);
      return id;
    },
    clearTimer: (id) => timers.delete(id),
  });

  controller.start();
  assert.equal(timers.size, 1);
  controller.stop();
  assert.equal(timers.size, 0);
});
