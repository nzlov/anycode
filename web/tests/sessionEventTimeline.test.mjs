import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  appendLiveEvent,
  eventAfterId,
  isEventAtOrAfter,
  prependOlderEvents,
  shouldRefreshSessionForEvent,
} from '../src/services/sessionEventTimeline.js';

const newest = {
  id: 'event-3',
  rawType: 'session.closed',
  createdAt: '2026-07-08T01:36:09Z',
};
const middle = {
  id: 'event-2',
  rawType: 'process.codex_event',
  createdAt: '2026-07-08T01:15:23Z',
};
const older = {
  id: 'event-1',
  rawType: 'process.codex_event',
  createdAt: '2026-07-07T15:09:14Z',
};

test('eventAfterId uses the current newest event as subscription cursor', () => {
  assert.equal(eventAfterId([middle, newest]), 'event-3');
  assert.equal(eventAfterId([]), '');
});

test('appendLiveEvent ignores duplicate history replay events', () => {
  const events = [middle, newest];

  assert.equal(appendLiveEvent(events, middle), events);
  assert.deepEqual(appendLiveEvent(events, older).map((event) => event.id), [
    'event-2',
    'event-3',
    'event-1',
  ]);
});

test('prependOlderEvents dedupes older page while preserving the viewport anchor order', () => {
  const events = [middle, newest];
  const next = prependOlderEvents(events, [older, middle]);

  assert.deepEqual(next.map((event) => event.id), ['event-1', 'event-2', 'event-3']);
});

test('shouldRefreshSessionForEvent refreshes only live session/workflow state events', () => {
  assert.equal(shouldRefreshSessionForEvent({ rawType: 'session.running' }, true), true);
  assert.equal(shouldRefreshSessionForEvent({ rawType: 'workflow.blocked' }, true), true);
  assert.equal(shouldRefreshSessionForEvent({ rawType: 'session.running' }, false), false);
  assert.equal(shouldRefreshSessionForEvent({ rawType: 'session.running' }, false, true), true);
  assert.equal(shouldRefreshSessionForEvent({ rawType: 'process.codex_event' }, true), false);
});

test('isEventAtOrAfter separates subscription replay from newly published events', () => {
  const openedAt = Date.parse('2026-07-08T01:00:00Z');

  assert.equal(isEventAtOrAfter({ createdAt: '2026-07-08T01:00:01Z' }, openedAt), true);
  assert.equal(isEventAtOrAfter({ createdAt: '2026-07-07T23:59:59Z' }, openedAt), false);
  assert.equal(isEventAtOrAfter({ createdAt: '' }, openedAt), false);
});
