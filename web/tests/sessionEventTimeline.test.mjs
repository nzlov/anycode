import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  appendLiveEvent,
  createKeyedLatestRequestTracker,
  createLatestRequestTracker,
  prependOlderEvents,
  shouldReconnectSubscription,
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

test('appendLiveEvent merges a live group delta with the same stable id', () => {
  const first = {
    ...middle,
    id: 'group:lifecycle:process-1',
    group: { count: 1, members: [older] },
  };
  const nextSnapshot = {
    ...first,
    group: { count: 1, members: [middle] },
  };

  const next = appendLiveEvent([first, newest], nextSnapshot);

  assert.equal(next.length, 2);
  assert.equal(next[0].group.count, 2);
  assert.deepEqual(
    next[0].group.members.map((event) => event.id),
    ['event-1', 'event-2'],
  );
});

test('prependOlderEvents restores historical members of an already live group', () => {
  const historical = {
    ...older,
    id: 'group:lifecycle:process-1',
    group: { count: 2, members: [older, middle] },
  };
  const live = {
    ...newest,
    id: historical.id,
    group: { count: 1, members: [newest] },
  };

  const next = prependOlderEvents([live], [historical]);

  assert.equal(next.length, 1);
  assert.equal(next[0].orderKey, older.orderKey);
  assert.deepEqual(
    next[0].group.members.map((event) => event.id),
    ['event-1', 'event-2', 'event-3'],
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

test('subscriptions validate pre-ack closes before deciding whether to reconnect', async () => {
  let validations = 0;
  const validAccessKey = async () => {
    validations += 1;
    return true;
  };
  assert.equal(
    await shouldReconnectSubscription(
      { acknowledged: true, completedByServer: false },
      validAccessKey,
    ),
    true,
  );
  assert.equal(validations, 0);
  assert.equal(
    await shouldReconnectSubscription(
      { acknowledged: false, completedByServer: false },
      validAccessKey,
    ),
    true,
  );
  assert.equal(validations, 1);
  assert.equal(
    await shouldReconnectSubscription(
      { acknowledged: false, completedByServer: false },
      async () => false,
    ),
    false,
  );
  assert.equal(
    await shouldReconnectSubscription(
      { acknowledged: false, completedByServer: false },
      async () => {
        throw new Error('health check unavailable');
      },
    ),
    true,
  );
  assert.equal(
    await shouldReconnectSubscription(
      { acknowledged: true, completedByServer: true },
      validAccessKey,
    ),
    true,
  );
  assert.equal(validations, 1);
});

test('latest request tracker invalidates stale requests after live state updates', () => {
  const tracker = createLatestRequestTracker();
  const request = tracker.next();

  tracker.invalidate();

  assert.equal(tracker.isCurrent(request), false);
  const refresh = tracker.next();
  assert.equal(tracker.isCurrent(refresh), true);
});

test('keyed request tracker rejects late card responses without affecting other cards', () => {
  const tracker = createKeyedLatestRequestTracker();
  const staleCardRequest = tracker.next('session-1');
  const otherCardRequest = tracker.next('session-2');

  tracker.invalidate('session-1');

  assert.equal(tracker.isCurrent('session-1', staleCardRequest), false);
  assert.equal(tracker.isCurrent('session-2', otherCardRequest), true);
  const currentCardRequest = tracker.next('session-1');
  assert.equal(tracker.isCurrent('session-1', currentCardRequest), true);
  tracker.clear();
  assert.equal(tracker.isCurrent('session-1', currentCardRequest), false);
});
