import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('prompt append editing uses a dedicated mutation and updates one append', () => {
  const serviceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const composableSource = readFileSync(
    new URL('../src/composables/useSessionDetail.ts', import.meta.url),
    'utf8',
  );

  assert.match(serviceSource, /mutation UpdatePromptAppend\(\$input: UpdatePromptAppendInput!\)/);
  assert.match(serviceSource, /updatePromptAppend\(input: \$input\)/);
  assert.match(serviceSource, /notify: false/);
  assert.match(composableSource, /current\.promptAppends\.map/);
  assert.match(composableSource, /prompt\.id === updated\.id/);
});

test('session detail opens a body-only dialog without local status gating', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  const openBlock = pageSource.slice(
    pageSource.indexOf('function openPromptAppendEditor'),
    pageSource.indexOf('async function savePromptAppendEdit'),
  );

  assert.match(pageSource, /@click="openPromptAppendEditor\(item\)"/);
  assert.match(pageSource, /v-model="promptEditBody"/);
  assert.match(pageSource, /附件保持不变/);
  assert.match(pageSource, /:disable="!canSavePromptAppendEdit"/);
  assert.match(pageSource, /body && body !== target\.body\.trim\(\)/);
  assert.doesNotMatch(openBlock, /session\.value\?\.status/);
});

test('failed prompt append save keeps the dialog and edited body intact', () => {
  const pageSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  const saveBlock = pageSource.slice(
    pageSource.indexOf('async function savePromptAppendEdit'),
    pageSource.indexOf('async function sendAppend'),
  );
  const catchBlock = saveBlock.slice(saveBlock.indexOf('catch'));

  assert.match(saveBlock, /await updatePromptAppendBody/);
  assert.match(saveBlock, /promptEditDialogOpen\.value = false/);
  assert.match(catchBlock, /promptEditError\.value/);
  assert.doesNotMatch(catchBlock, /promptEditDialogOpen\.value = false/);
  assert.doesNotMatch(catchBlock, /promptEditBody\.value = ''/);
});
