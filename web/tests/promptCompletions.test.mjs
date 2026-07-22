import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  activePromptCompletion,
  applyPromptCompletion,
  filterSlashCommands,
  formatFileMention,
  promptMatchSegments,
} from '../src/services/promptCompletionText.js';

const commands = [
  { name: '/review', description: 'review', acceptsArgs: true, requiresThread: false },
  { name: '/compact', description: 'compact', acceptsArgs: false, requiresThread: true },
  { name: '/goal', description: 'goal', acceptsArgs: true, requiresThread: true },
  { name: '/plan', description: 'plan', acceptsArgs: true, requiresThread: false },
];

const promptComposerSource = readFileSync(
  new URL('../src/components/PromptComposer.vue', import.meta.url),
  'utf8',
);
const sessionServiceSource = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');
const newSessionSource = readFileSync(
  new URL('../src/components/NewSessionDialog.vue', import.meta.url),
  'utf8',
);
const sessionDetailSource = readFileSync(
  new URL('../src/components/SessionDetailView.vue', import.meta.url),
  'utf8',
);

test('prompt completion refreshes from the Quasar model update event', () => {
  assert.match(promptComposerSource, /@update:model-value="queuePromptCompletionRefresh"/);
});

test('completion activates only for a slash or mention token at the cursor', () => {
  assert.deepEqual(activePromptCompletion('run /rv', 7), {
    kind: 'command',
    query: 'rv',
    start: 4,
    end: 7,
  });
  assert.deepEqual(activePromptCompletion('inspect @src/ma'), {
    kind: 'file',
    query: 'src/ma',
    start: 8,
    end: 15,
  });
  assert.equal(activePromptCompletion('mail@example.com'), null);
  assert.equal(activePromptCompletion('/review done'), null);
});

test('slash command filtering is fuzzy and respects thread requirements', () => {
  assert.deepEqual(
    filterSlashCommands(commands, 'rv', false).map((item) => item.name),
    ['/review'],
  );
  assert.deepEqual(
    filterSlashCommands(commands, '', false).map((item) => item.name),
    ['/review', '/plan'],
  );
  assert.deepEqual(
    filterSlashCommands(commands, 'cpt', true).map((item) => item.name),
    ['/compact'],
  );
  assert.deepEqual(
    filterSlashCommands(commands, 'gol', true).map((item) => item.name),
    ['/goal'],
  );
  assert.deepEqual(
    filterSlashCommands(commands, 'pln', false).map((item) => item.name),
    ['/plan'],
  );
});

test('completion replacement preserves surrounding prompt text', () => {
  const range = activePromptCompletion('check @src/ma next', 13);
  assert.ok(range);
  assert.equal(
    applyPromptCompletion('check @src/ma next', range, '@src/main.go'),
    'check @src/main.go next',
  );
  assert.equal(formatFileMention('src/main.go'), '@src/main.go');
  assert.equal(formatFileMention('docs/a b.md'), '@"docs/a b.md"');
});

test('file match indices are grouped into display segments', () => {
  assert.deepEqual(promptMatchSegments('main.go', [0, 1, 2, 3]), [
    { text: 'main', matched: true },
    { text: '.go', matched: false },
  ]);
});

test('selected file completions travel as structured prompt mentions', () => {
  assert.match(promptComposerSource, /'update:mentions'/);
  assert.match(promptComposerSource, /\{ path: item\.file\.path \}/);
  assert.match(promptComposerSource, /props\.mentions\.filter/);
  assert.match(newSessionSource, /v-model:mentions="mentions"/);
  assert.match(sessionDetailSource, /v-model:mentions="appendMentions"/);
  assert.match(sessionServiceSource, /input\.mentions = mentions/);
});
