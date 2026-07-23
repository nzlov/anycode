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

test('attachment button opens the shared file picker', () => {
  const composerSource = readFileSync(
    new URL('../src/components/PromptComposer.vue', import.meta.url),
    'utf8',
  );

  assert.match(
    composerSource,
    /<q-btn[\s\S]*?icon="attach_file"[\s\S]*?aria-label="添加附件"[\s\S]*?@click="filePickerRef\?\.pickFiles\(\$event\)"/,
  );
  assert.match(
    composerSource,
    /<q-file\s+ref="filePickerRef"[\s\S]*?v-model="filesModel"[\s\S]*?multiple[\s\S]*?append[\s\S]*?class="hidden"/,
  );
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

test('image attachments use viewport-relative previews without scrolling', () => {
  const composerSource = readFileSync(
    new URL('../src/components/PromptComposer.vue', import.meta.url),
    'utf8',
  );
  const stylesSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(
    composerSource,
    /<div\s+v-if="isImageFile\(file\)"\s+class="attachment-image-item">[\s\S]*?class="attachment-image-preview"[\s\S]*?<button[\s\S]*?class="attachment-image-trigger"/,
  );
  assert.doesNotMatch(composerSource, /<q-chip\s+v-if="isImageFile\(file\)"/);
  assert.match(
    composerSource,
    /class="attachment-image-trigger"[\s\S]*?class="attachment-thumbnail"[\s\S]*?class="attachment-image-name ellipsis"/,
  );
  assert.match(
    composerSource,
    /<q-btn[\s\S]*?class="attachment-image-remove"[\s\S]*?:aria-label="`移除图片 \$\{file\.name\}`"[\s\S]*?@click\.stop="removeFile\(file\)"/,
  );
  assert.match(
    composerSource,
    /<q-tooltip\s+v-if="!previewOpen"[\s\S]*?class="attachment-image-tooltip"[\s\S]*?class="attachment-hover-preview"/,
  );
  assert.match(composerSource, /URL\.createObjectURL\(file\)/);
  assert.match(composerSource, /URL\.revokeObjectURL\(url\)/);
  assert.match(
    stylesSource,
    /\.attachment-image-item\s*\{[^}]*width:\s*fit-content[^}]*max-width:\s*min\(16vw,\s*10vh\)[^}]*flex-direction:\s*column/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-image-preview\s*\{[^}]*position:\s*relative[^}]*width:\s*fit-content[^}]*max-width:\s*100%/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-image-trigger\s*\{[^}]*width:\s*fit-content[^}]*max-width:\s*100%[^}]*padding:\s*0[^}]*border:\s*0[^}]*background:\s*transparent/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-thumbnail\s*\{[^}]*width:\s*auto[^}]*max-width:\s*100%[^}]*height:\s*auto[^}]*max-height:\s*min\(16vw,\s*10vh\)[^}]*object-fit:\s*contain/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-image-remove\s*\{[^}]*position:\s*absolute[^}]*top:\s*4px[^}]*right:\s*4px/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-image-tooltip\s*\{[^}]*width:\s*min\(40vw,\s*70vh\)[^}]*max-width:\s*96vw/s,
  );
  assert.match(stylesSource, /\.attachment-preview-card\s*\{[^}]*height:\s*90dvh/s);
  assert.match(
    stylesSource,
    /\.attachment-preview-body\s*\{[^}]*overflow:\s*hidden[^}]*padding:\s*0/s,
  );
  assert.match(
    stylesSource,
    /\.attachment-preview-media\s*\{[^}]*width:\s*100%[^}]*height:\s*100%[^}]*object-fit:\s*contain/s,
  );
});
