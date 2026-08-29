import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

function readSource(path) {
  return readFileSync(new URL(path, import.meta.url), 'utf8');
}

const serviceSource = readSource('../src/services/sessionSides.ts');
const dialogSource = readSource('../src/components/SessionSideDialog.vue');
const inputSource = readSource('../src/components/SessionSidePromptInput.vue');
const promptComposerSource = readSource('../src/components/PromptComposer.vue');
const composerSource = readSource('../src/components/CodexPromptComposer.vue');
const detailSource = readSource('../src/components/SessionDetailView.vue');

test('Side calls ephemeral backend operations and consumes the shared transcript contract', () => {
  assert.match(serviceSource, /mutation StartSessionSide/);
  assert.match(serviceSource, /mutation ContinueSessionSide/);
  assert.match(serviceSource, /mutation StopSessionSide/);
  assert.match(serviceSource, /subscription SessionSideEvents/);
  assert.match(serviceSource, /normalizeTranscriptEvent/);
  assert.match(dialogSource, /reduceTranscriptEvents/);
  assert.match(dialogSource, /<SessionEventMessage/);
});

test('Side entry is immediately left of quick replies in the append composer', () => {
  const sideSlot = composerSource.indexOf('<slot name="before-quick-actions" />');
  const quickReply = composerSource.indexOf('icon="bolt"');
  assert.ok(sideSlot >= 0 && quickReply > sideSlot);
  assert.match(detailSource, /#before-quick-actions/);
  assert.match(detailSource, /aria-label="Side 临时提问"/);
  assert.match(detailSource, /<SessionSideDialog/);
});

test('Side dialog shows direct input when empty, a closeable list, and FAB composition after use', () => {
  assert.match(dialogSource, /v-else-if="sides\.length"/);
  assert.match(dialogSource, /v-for="side in sides"/);
  assert.match(dialogSource, /@click\.stop="closeSide\(side\)"/);
  assert.match(dialogSource, /\bfab\b/);
  assert.match(dialogSource, /v-else class="side-dialog__initial-prompt"/);
  assert.match(dialogSource, /stopSessionSide\(side\.processRunId\)/);
  assert.match(inputSource, /aria-label="发送 Side 提问"/);
});

test('Side uses the collapsible common composer without unsupported run controls', () => {
  assert.match(inputSource, /<PromptComposer/);
  assert.match(inputSource, /\bcollapsible\b/);
  assert.match(inputSource, /:show-config="false"/);
  assert.match(inputSource, /:allow-attachments="false"/);
  assert.doesNotMatch(inputSource, /\b(model|effort|permission)=/);
  assert.match(promptComposerSource, /showConfig\?: boolean/);
  assert.match(promptComposerSource, /allowAttachments\?: boolean/);
});

test('Side dialog caps its height and keeps the composer outside the event scroller', () => {
  assert.match(dialogSource, /max-height: min\(760px, calc\(100dvh - 48px\)\) !important/);
  assert.match(dialogSource, /\.side-dialog__detail[\s\S]*overflow: hidden/);
  assert.match(dialogSource, /\.side-dialog__events,[\s\S]*overflow-y: auto/);
});

test('Side history remains current-page memory only', () => {
  assert.doesNotMatch(dialogSource, /localStorage|sessionStorage|indexedDB/);
  assert.doesNotMatch(serviceSource, /localStorage|sessionStorage|indexedDB/);
  assert.match(dialogSource, /sides\.value = sides\.value\.filter/);
});
