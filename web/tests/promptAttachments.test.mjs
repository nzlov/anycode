import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { filesFromTransfer } from '../src/services/promptAttachments.js';

test('clipboard files are read from the transfer file list', () => {
  const image = { name: 'clipboard.png' };

  assert.deepEqual(filesFromTransfer({ files: [image], items: [] }), [image]);
});

test('clipboard file items are used when the file list is empty', () => {
  const document = { name: 'notes.txt' };
  const transfer = {
    files: [],
    items: [
      { kind: 'string', getAsFile: () => null },
      { kind: 'file', getAsFile: () => document },
      { kind: 'file', getAsFile: () => null },
    ],
  };

  assert.deepEqual(filesFromTransfer(transfer), [document]);
});

test('shared prompt input appends pasted files without blocking normal text paste', () => {
  const composerSource = readFileSync(
    new URL('../src/components/PromptComposer.vue', import.meta.url),
    'utf8',
  );

  assert.match(composerSource, /@paste="onPaste"/);
  assert.match(composerSource, /appendFiles\(filesFromTransfer\(event\.clipboardData\)\)/);
  assert.doesNotMatch(composerSource, /function onPaste[\s\S]*?preventDefault\(\)/);
});
