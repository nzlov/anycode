import assert from 'node:assert/strict';
import { test } from 'node:test';

import { reduceSessionTimelineEvents } from '../src/services/sessionTimelineReducer.js';

function commandEvent(id, orderKey, correlationId, phase, content = {}) {
  return {
    id,
    orderKey,
    correlationId,
    phase,
    occurredAt: `2026-07-12T00:00:${orderKey}Z`,
    content: {
      __typename: 'SessionCommandContent',
      command: '',
      output: '',
      exitCode: null,
      durationMs: null,
      ...content,
    },
  };
}

function toolResult(id, orderKey, correlationId, output) {
  return {
    id,
    orderKey,
    correlationId,
    phase: 'completed',
    occurredAt: `2026-07-12T00:00:${orderKey}Z`,
    content: {
      __typename: 'SessionToolContent',
      qualifiedName: '',
      category: 'generic',
      input: { format: 'plain', text: '' },
      output: { format: 'plain', text: output },
      images: [],
    },
  };
}

function messageEvent(id, orderKey, text) {
  return {
    id,
    orderKey,
    correlationId: '',
    phase: 'standalone',
    occurredAt: `2026-07-12T00:00:${orderKey}Z`,
    content: {
      __typename: 'SessionTextMessageContent',
      role: 'assistant',
      text,
      format: 'markdown',
      images: [],
    },
  };
}

test('completed updates the started item without changing normal message order', () => {
  const items = reduceSessionTimelineEvents([
    commandEvent('start-a', '01', 'call-a', 'started', { command: 'npm test' }),
    messageEvent('message-b', '02', 'working'),
    commandEvent('complete-a', '03', 'call-a', 'completed', { output: 'passed' }),
  ]);

  assert.deepEqual(
    items.map((item) => item.id),
    ['call-a', 'message-b'],
  );
  assert.equal(items[0].orderKey, '01');
  assert.equal(items[0].phase, 'completed');
  assert.equal(items[0].content.output, 'passed');
  assert.deepEqual(items[0].sourceEventIds, ['start-a', 'complete-a']);
});

test('interleaved operations merge only by correlation id', () => {
  const items = reduceSessionTimelineEvents([
    commandEvent('start-a', '01', 'call-a', 'started', { command: 'same command' }),
    commandEvent('start-b', '02', 'call-b', 'started', { command: 'same command' }),
    commandEvent('complete-b', '03', 'call-b', 'completed', { output: 'b output' }),
    commandEvent('complete-a', '04', 'call-a', 'completed', { output: 'a output' }),
  ]);

  assert.deepEqual(
    items.map((item) => [item.id, item.content.output]),
    [
      ['call-a', 'a output'],
      ['call-b', 'b output'],
    ],
  );
});

test('completed without a loaded start remains visible', () => {
  const items = reduceSessionTimelineEvents([toolResult('complete-a', '03', 'call-a', 'output')]);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'call-a');
  assert.equal(items[0].content.output.text, 'output');
});

test('loading an older start later anchors the merged item at the start', () => {
  const completed = commandEvent('complete-a', '03', 'call-a', 'completed', {
    output: 'output',
  });
  const start = commandEvent('start-a', '01', 'call-a', 'started', { command: 'go test ./...' });

  const items = reduceSessionTimelineEvents([completed, start]);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'call-a');
  assert.equal(items[0].orderKey, '01');
  assert.equal(items[0].content.command, 'go test ./...');
  assert.equal(items[0].content.output, 'output');
});

test('typed command completion keeps exit metadata and terminal phase', () => {
  const items = reduceSessionTimelineEvents([
    commandEvent('start-a', '01', 'call-a', 'started', { command: 'exit 7' }),
    commandEvent('complete-a', '02', 'call-a', 'failed', {
      output: 'failed',
      exitCode: 7,
      durationMs: 125,
    }),
  ]);

  assert.equal(items.length, 1);
  assert.equal(items[0].phase, 'failed');
  assert.equal(items[0].content.command, 'exit 7');
  assert.equal(items[0].content.output, 'failed');
  assert.equal(items[0].content.exitCode, 7);
  assert.equal(items[0].content.durationMs, 125);
});

test('standalone events never merge even when their content is identical', () => {
  const items = reduceSessionTimelineEvents([
    messageEvent('message-a', '01', 'same'),
    messageEvent('message-b', '02', 'same'),
  ]);

  assert.deepEqual(
    items.map((item) => item.id),
    ['message-a', 'message-b'],
  );
});

test('proxied event content is copied into a plain timeline item', () => {
  const event = messageEvent('message-a', '01', 'proxied');
  const images = new Proxy([{ src: '/preview/image-a', detail: 'auto' }], {});
  event.content = new Proxy({ ...event.content, images }, {});

  const [item] = reduceSessionTimelineEvents([event]);

  assert.deepEqual(item.content, {
    __typename: 'SessionTextMessageContent',
    role: 'assistant',
    text: 'proxied',
    format: 'markdown',
    images: [{ src: '/preview/image-a', detail: 'auto' }],
  });
  assert.notEqual(item.content, event.content);
  assert.notEqual(item.content.images, event.content.images);
});
