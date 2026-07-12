import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  appendLiveEvent,
  createLatestRequestTracker,
  mergeSnapshotEvents,
  prependOlderEvents,
  shouldReconnectAfterClose,
  shouldReconnectCardStream,
  sortSessionEvents,
} from '../src/services/sessionEventTimeline.js';

const newest = {
  id: 'event-3',
  orderKey: '03',
};
const middle = {
  id: 'event-2',
  orderKey: '02',
};
const older = {
  id: 'event-1',
  orderKey: '01',
};

test('appendLiveEvent ignores duplicate history replay events', () => {
  const events = [middle, newest];

  assert.equal(appendLiveEvent(events, middle), events);
  assert.deepEqual(
    appendLiveEvent(events, older).map((event) => event.id),
    ['event-2', 'event-3', 'event-1'],
  );
});

test('prependOlderEvents dedupes older page while preserving the viewport anchor order', () => {
  const events = [middle, newest];
  const next = prependOlderEvents(events, [older, middle]);

  assert.deepEqual(
    next.map((event) => event.id),
    ['event-1', 'event-2', 'event-3'],
  );
});

test('mergeSnapshotEvents preserves older pages and live events while replacing duplicate snapshot entries', () => {
  const snapshotMiddle = { ...middle, title: 'snapshot' };
  const liveMiddle = { ...middle, title: 'live' };
  const liveNewest = { ...newest, title: 'live newest' };

  const next = mergeSnapshotEvents([snapshotMiddle], [older, liveMiddle], [liveNewest]);

  assert.deepEqual(
    next.map((event) => event.id),
    ['event-1', 'event-2', 'event-3'],
  );
  assert.equal(next.find((event) => event.id === 'event-2').title, 'live');
});

test('mergeSnapshotEvents preserves backend order keys across a page boundary', () => {
  const started = { ...older, id: 'z-started', orderKey: '01' };
  const completed = { ...middle, id: 'a-completed', orderKey: '02' };

  const next = mergeSnapshotEvents([completed], [started, completed], []);

  assert.deepEqual(
    sortSessionEvents(next).map((event) => event.id),
    ['z-started', 'a-completed'],
  );
});

test('shouldReconnectAfterClose stops confirmed auth failures and normal server completion', () => {
  assert.equal(shouldReconnectAfterClose(true, false, false), true);
  assert.equal(shouldReconnectAfterClose(false, true, false), true);
  assert.equal(shouldReconnectAfterClose(false, undefined, false), true);
  assert.equal(shouldReconnectAfterClose(false, false, false), false);
  assert.equal(shouldReconnectAfterClose(true, undefined, true), false);
});

test('card streams validate pre-ack closes before deciding whether to reconnect', async () => {
  let validations = 0;
  const validAccessKey = async () => {
    validations += 1;
    return true;
  };
  assert.equal(
    await shouldReconnectCardStream(
      { acknowledged: true, completedByServer: false },
      validAccessKey,
    ),
    true,
  );
  assert.equal(validations, 0);
  assert.equal(
    await shouldReconnectCardStream(
      { acknowledged: false, completedByServer: false },
      validAccessKey,
    ),
    true,
  );
  assert.equal(validations, 1);
  assert.equal(
    await shouldReconnectCardStream(
      { acknowledged: false, completedByServer: false },
      async () => false,
    ),
    false,
  );
  assert.equal(
    await shouldReconnectCardStream({ acknowledged: false, completedByServer: false }, async () => {
      throw new Error('health check unavailable');
    }),
    true,
  );
  assert.equal(
    await shouldReconnectCardStream(
      { acknowledged: true, completedByServer: true },
      validAccessKey,
    ),
    false,
  );
  assert.equal(validations, 1);
});

test('sortSessionEvents follows immutable backend order keys', () => {
  const completed = { ...newest, id: 'a-completed', orderKey: '02' };
  const started = { ...middle, id: 'z-started', orderKey: '01' };

  assert.deepEqual(
    sortSessionEvents([started, completed]).map((event) => event.id),
    ['z-started', 'a-completed'],
  );
});

test('latest request tracker invalidates stale snapshots after live state updates', () => {
  const tracker = createLatestRequestTracker();
  const snapshot = tracker.next();

  tracker.invalidate();

  assert.equal(tracker.isCurrent(snapshot), false);
  const refresh = tracker.next();
  assert.equal(tracker.isCurrent(refresh), true);
});
