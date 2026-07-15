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

test('shared prompt shell owns pasted files without relying on q-input listener forwarding', () => {
  const composerSource = readFileSync(
    new URL('../src/components/PromptComposer.vue', import.meta.url),
    'utf8',
  );

  assert.match(composerSource, /class="prompt-shell"[\s\S]*?@paste="onPaste"/);
  assert.doesNotMatch(composerSource, /<q-input[\s\S]*?@paste="onPaste"[\s\S]*?\/>/);
  assert.match(composerSource, /appendFiles\(filesFromTransfer\(event\.clipboardData\)\)/);
  assert.doesNotMatch(composerSource, /function onPaste[\s\S]*?preventDefault\(\)/);
});

test('clipboard extraction keeps every copied file type in transfer order', () => {
  const image = { name: 'screenshot.png', type: 'image/png' };
  const archive = { name: 'source.tar.gz', type: 'application/gzip' };
  const document = { name: 'notes.txt', type: 'text/plain' };

  assert.deepEqual(filesFromTransfer({ files: [image, archive, document], items: [] }), [
    image,
    archive,
    document,
  ]);
});

test('new session stages each file from the shared prompt state once', () => {
  const newSessionSource = readFileSync(
    new URL('../src/components/NewSessionDialog.vue', import.meta.url),
    'utf8',
  );

  assert.match(newSessionSource, /v-model:files="files"/);
  assert.match(
    newSessionSource,
    /const selectedFiles = \[\.\.\.files\.value\][\s\S]*?for \(const file of selectedFiles\)[\s\S]*?await stageAttachment\(file\)/,
  );
  assert.equal(newSessionSource.match(/await stageAttachment\(file\)/g)?.length, 1);
});
