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

test('appendLiveEvent replaces an empty reasoning start with completed content', () => {
  const started = {
    id: 'reasoning-1',
    orderKey: '01',
    occurredAt: '2026-07-22T10:00:00Z',
    phase: 'started',
    content: { __typename: 'TranscriptReasoningContent', text: '' },
  };
  const progress = {
    ...started,
    phase: 'progress',
    content: { __typename: 'TranscriptReasoningContent', text: 'thinking' },
  };
  const completed = {
    ...started,
    phase: 'completed',
    content: { __typename: 'TranscriptReasoningContent', text: 'final reasoning' },
  };

  const withProgress = appendLiveEvent([started], progress);
  const next = appendLiveEvent(withProgress, completed);

  assert.equal(withProgress[0].content.text, 'thinking');
  assert.equal(next[0].phase, 'completed');
  assert.equal(next[0].content.text, 'final reasoning');
});

test('appendLiveEvent keeps command identity while accumulating output deltas', () => {
  const started = {
    id: 'command-1',
    orderKey: '01',
    occurredAt: '2026-07-22T10:00:00Z',
    phase: 'started',
    content: {
      __typename: 'TranscriptCommandContent',
      kind: 'exec',
      commands: [{ command: 'go test ./...', workdir: '/workspace', hasOutput: false, output: '' }],
    },
  };
  const delta = (output) => ({
    ...started,
    phase: 'progress',
    content: {
      __typename: 'TranscriptCommandContent',
      kind: 'exec',
      commands: [{ command: '', workdir: '', hasOutput: true, output }],
    },
  });

  const first = appendLiveEvent([started], delta('pass'));
  const second = appendLiveEvent(first, delta('ed'));

  assert.equal(second[0].content.commands[0].command, 'go test ./...');
  assert.equal(second[0].content.commands[0].workdir, '/workspace');
  assert.equal(second[0].content.commands[0].output, 'passed');
});

test('appendLiveEvent updates a questions tool call with its completed output', () => {
  const started = {
    id: 'question-1',
    orderKey: '01',
    occurredAt: '2026-07-22T10:00:00Z',
    phase: 'started',
    content: {
      __typename: 'TranscriptToolContent',
      qualifiedName: 'questions',
      category: 'dynamic',
      input: { format: 'json', text: '{"questions":[]}' },
      output: { format: 'json', text: '' },
      images: [],
    },
  };
  const completed = {
    ...started,
    phase: 'completed',
    content: {
      ...started.content,
      output: { format: 'json', text: '{"answers":[]}' },
    },
  };

  const [event] = appendLiveEvent([started], completed);

  assert.equal(event.phase, 'completed');
  assert.equal(event.content.output.text, '{"answers":[]}');
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
