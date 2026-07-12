import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  latestSessionEventPageInput,
  olderSessionEventCursor,
} from '../src/services/sessionEventPaging.ts';

test('latestSessionEventPageInput requests the newest event page directly', () => {
  assert.deepEqual(latestSessionEventPageInput('session-1', '', 50), {
    sessionId: 'session-1',
    limit: 50,
  });
});

test('latestSessionEventPageInput includes before cursor for older pages', () => {
  assert.deepEqual(latestSessionEventPageInput('session-1', 'event-40', 50), {
    sessionId: 'session-1',
    beforeEventId: 'event-40',
    limit: 50,
  });
});

test('olderSessionEventCursor uses the backend cursor', () => {
  assert.equal(olderSessionEventCursor({ nextCursor: 'event-40' }), 'event-40');
  assert.equal(olderSessionEventCursor({ nextCursor: '' }), null);
});

test('useSessionDetail loads older events with cursor input instead of page number', () => {
  const source = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );
  const match =
    /async function loadOlderEvents\(\)(?:: Promise<[^>]+>)? \{(?<body>[\s\S]*?)\n\s{2}\}/.exec(
      source,
    );

  assert.ok(match?.groups?.body);
  assert.match(match.groups.body, /olderSessionEventCursor\(eventsPageInfo\.value\)/);
  assert.match(
    match.groups.body,
    /getSessionTimelinePage\(sessionId, beforeEventId, eventPageSize\)/,
  );
  assert.doesNotMatch(match.groups.body, /page\s*[,+)]/);
  assert.match(match.groups.body, /return result\.pageInfo\.nextCursor/);
});
