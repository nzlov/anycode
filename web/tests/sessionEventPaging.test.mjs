import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  latestTranscriptPageInput,
  olderTranscriptCursor,
} from '../src/services/sessionEventPaging.ts';

test('latestTranscriptPageInput requests the newest event page directly', () => {
  assert.deepEqual(latestTranscriptPageInput('session-1', '', 50), {
    sessionId: 'session-1',
    limit: 50,
  });
});

test('latestTranscriptPageInput includes before cursor for older pages', () => {
  assert.deepEqual(latestTranscriptPageInput('session-1', 'event-40', 50), {
    sessionId: 'session-1',
    beforeCursor: 'event-40',
    limit: 50,
  });
});

test('latestTranscriptPageInput includes an optional message role filter', () => {
  assert.deepEqual(latestTranscriptPageInput('session-1', '', 10, 'assistant'), {
    sessionId: 'session-1',
    messageRole: 'assistant',
    limit: 10,
  });
});

test('olderTranscriptCursor uses the backend cursor', () => {
  assert.equal(olderTranscriptCursor({ nextCursor: 'event-40' }), 'event-40');
  assert.equal(olderTranscriptCursor({ nextCursor: '' }), null);
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
  assert.match(match.groups.body, /olderTranscriptCursor\(eventsPageInfo\.value\)/);
  assert.match(
    match.groups.body,
    /getSessionTranscriptPage\(sessionId, beforeCursor, eventPageSize\)/,
  );
  assert.doesNotMatch(match.groups.body, /page\s*[,+)]/);
  assert.match(match.groups.body, /return result\.pageInfo\.nextCursor/);
});
