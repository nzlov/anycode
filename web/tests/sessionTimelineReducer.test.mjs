import assert from 'node:assert/strict';
import { test } from 'node:test';

import { reduceTranscriptEvents } from '../src/services/sessionTimelineReducer.js';

function commandEvent(id, orderKey, correlationId, phase, content = {}) {
  return {
    id,
    orderKey,
    correlationId,
    phase,
    occurredAt: `2026-07-12T00:00:${orderKey}Z`,
    content: {
      __typename: 'TranscriptCommandContent',
      kind: 'shell',
      commands: [],
      durationMs: null,
      ...content,
    },
  };
}

function commandInvocation(command, workdir = '', result = {}) {
  return {
    command,
    workdir,
    hasOutput: false,
    output: '',
    exitCode: null,
    durationMs: null,
    ...result,
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
      __typename: 'TranscriptToolContent',
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
      __typename: 'TranscriptMessageContent',
      role: 'assistant',
      text,
      format: 'markdown',
      images: [],
    },
  };
}

test('completed updates the started item without changing normal message order', () => {
  const items = reduceTranscriptEvents([
    commandEvent('start-a', '01', 'call-a', 'started', {
      commands: [commandInvocation('npm test')],
    }),
    messageEvent('message-b', '02', 'working'),
    commandEvent('complete-a', '03', 'call-a', 'completed', {
      commands: [commandInvocation('npm test', '', { hasOutput: true, output: 'passed' })],
    }),
  ]);

  assert.deepEqual(
    items.map((item) => item.id),
    ['call-a', 'message-b'],
  );
  assert.equal(items[0].orderKey, '01');
  assert.equal(items[0].phase, 'completed');
  assert.equal(items[0].content.commands[0].output, 'passed');
  assert.deepEqual(items[0].sourceEventIds, ['start-a', 'complete-a']);
});

test('exec completion keeps each returned output with its command', () => {
  const commands = [
    {
      command: 'npm test',
      workdir: '/workspace/web',
      hasOutput: false,
      output: '',
      exitCode: null,
      durationMs: null,
    },
    {
      command: 'go test ./...',
      workdir: '/workspace',
      hasOutput: false,
      output: '',
      exitCode: null,
      durationMs: null,
    },
  ];
  const completedCommands = [
    { ...commands[0], hasOutput: true, output: 'web passed', exitCode: 0, durationMs: 100 },
    { ...commands[1], hasOutput: true, output: 'go failed', exitCode: 1, durationMs: 200 },
  ];

  const items = reduceTranscriptEvents([
    commandEvent('start-exec', '01', 'call-exec', 'started', { kind: 'exec', commands }),
    commandEvent('complete-exec', '02', 'call-exec', 'failed', {
      kind: 'exec',
      commands: completedCommands,
    }),
  ]);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'call-exec');
  assert.equal(items[0].phase, 'failed');
  assert.deepEqual(items[0].content.commands, completedCommands);
  assert.deepEqual(items[0].sourceEventIds, ['start-exec', 'complete-exec']);
});

test('interleaved operations merge only by correlation id', () => {
  const items = reduceTranscriptEvents([
    commandEvent('start-a', '01', 'call-a', 'started', {
      commands: [commandInvocation('same command')],
    }),
    commandEvent('start-b', '02', 'call-b', 'started', {
      commands: [commandInvocation('same command')],
    }),
    commandEvent('complete-b', '03', 'call-b', 'completed', {
      commands: [commandInvocation('same command', '', { hasOutput: true, output: 'b output' })],
    }),
    commandEvent('complete-a', '04', 'call-a', 'completed', {
      commands: [commandInvocation('same command', '', { hasOutput: true, output: 'a output' })],
    }),
  ]);

  assert.deepEqual(
    items.map((item) => [item.id, item.content.commands[0].output]),
    [
      ['call-a', 'a output'],
      ['call-b', 'b output'],
    ],
  );
});

test('completed without a loaded start remains visible', () => {
  const items = reduceTranscriptEvents([toolResult('complete-a', '03', 'call-a', 'output')]);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'call-a');
  assert.equal(items[0].content.output.text, 'output');
});

test('loading an older start later anchors the merged item at the start', () => {
  const completed = commandEvent('complete-a', '03', 'call-a', 'completed', {
    commands: [
      commandInvocation('go test ./...', '/workspace', { hasOutput: true, output: 'output' }),
    ],
  });
  const start = commandEvent('start-a', '01', 'call-a', 'started', {
    commands: [commandInvocation('go test ./...', '/workspace')],
  });

  const items = reduceTranscriptEvents([completed, start]);

  assert.equal(items.length, 1);
  assert.equal(items[0].id, 'call-a');
  assert.equal(items[0].orderKey, '01');
  assert.deepEqual(items[0].content.commands, [
    commandInvocation('go test ./...', '/workspace', { hasOutput: true, output: 'output' }),
  ]);
  assert.equal(items[0].content.commands[0].output, 'output');
});

test('typed command completion keeps exit metadata and terminal phase', () => {
  const items = reduceTranscriptEvents([
    commandEvent('start-a', '01', 'call-a', 'started', {
      commands: [commandInvocation('exit 7')],
    }),
    commandEvent('complete-a', '02', 'call-a', 'failed', {
      commands: [
        commandInvocation('exit 7', '', {
          hasOutput: true,
          output: 'failed',
          exitCode: 7,
          durationMs: 125,
        }),
      ],
      durationMs: 125,
    }),
  ]);

  assert.equal(items.length, 1);
  assert.equal(items[0].phase, 'failed');
  assert.equal(items[0].content.commands[0].command, 'exit 7');
  assert.equal(items[0].content.commands[0].output, 'failed');
  assert.equal(items[0].content.commands[0].exitCode, 7);
  assert.equal(items[0].content.commands[0].durationMs, 125);
  assert.equal(items[0].content.durationMs, 125);
});

test('standalone events never merge even when their content is identical', () => {
  const items = reduceTranscriptEvents([
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

  const [item] = reduceTranscriptEvents([event]);

  assert.deepEqual(item.content, {
    __typename: 'TranscriptMessageContent',
    role: 'assistant',
    text: 'proxied',
    format: 'markdown',
    images: [{ src: '/preview/image-a', detail: 'auto' }],
  });
  assert.notEqual(item.content, event.content);
  assert.notEqual(item.content.images, event.content.images);
});
