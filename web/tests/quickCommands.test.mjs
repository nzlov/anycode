import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { appendQuickCommand } from '../src/services/quickCommandText.js';
import {
  prependQuickCommand,
  removeQuickCommandById,
  shouldApplyQuickCommandSnapshot,
} from '../src/services/quickCommandState.js';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('selected quick command fills an empty prompt or appends a new paragraph', () => {
  assert.equal(appendQuickCommand('', '检查测试'), '检查测试');
  assert.equal(appendQuickCommand('先修复问题', '检查测试'), '先修复问题\n\n检查测试');
});

test('duplicate command text remains independently addressable by id', () => {
  const commands = [
    { id: 'command-1', content: '检查测试' },
    { id: 'command-2', content: '检查测试' },
  ];
  assert.deepEqual(removeQuickCommandById(commands, 'command-1'), [commands[1]]);
  assert.deepEqual(prependQuickCommand(commands, commands[1], 20), commands);
});

test('stale page snapshots cannot overwrite a completed mutation', () => {
  assert.equal(shouldApplyQuickCommandSnapshot(2, 2, 3, 3), true);
  assert.equal(shouldApplyQuickCommandSnapshot(1, 2, 3, 3), false);
  assert.equal(shouldApplyQuickCommandSnapshot(2, 2, 2, 3), false);
});

test('each quick command consumer owns its pagination and blocks loads during mutations', () => {
  const composableSource = readSource('../src/composables/useQuickCommands.ts');
  const functionStart = composableSource.indexOf('export function useQuickCommands()');
  const stateStart = composableSource.indexOf('const quickCommands = ref<QuickCommand[]>([])');

  assert.ok(functionStart >= 0 && stateStart > functionStart);
  assert.match(composableSource, /if \(quickCommandsMutating\.value > 0\) return/);
  assert.match(composableSource, /mutationVersion \+= 1/);
});

test('quick reply always opens on the newest first page and disables stale loading items', () => {
  const composerSource = readSource('../src/components/CodexPromptComposer.vue');
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');

  assert.match(composerSource, /loadQuickCommands\(\{ force: true, page: 1 \}\)/);
  assert.doesNotMatch(composerSource, /onMounted\([\s\S]*?loadQuickCommands/);
  assert.match(composerSource, /:disable="quickCommandsLoading"/);
  assert.match(settingsSource, /quickCommandsMutating > 0/);
});

test('global settings load quick commands only while the quick command section is visible', () => {
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');

  assert.doesNotMatch(settingsSource, /onMounted\([\s\S]*?loadQuickCommands/);
  assert.match(settingsSource, /watch\(activeSection/);
  assert.match(settingsSource, /section !== 'quick_commands' \|\| !props\.modelValue/);
  assert.match(settingsSource, /activeSection\.value === 'quick_commands'/);
});

test('global settings expose quick command navigation, add FAB, and item deletion', () => {
  const layoutSource = readSource('../src/layouts/MainLayout.vue');
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');

  assert.match(layoutSource, /name="settings"/);
  assert.match(layoutSource, /<GlobalSettingsDialog/);
  assert.match(settingsSource, /class="global-settings-grid"/);
  assert.match(settingsSource, /快捷指令/);
  assert.match(settingsSource, /\bfab\b/);
  assert.match(settingsSource, /icon="delete_outline"/);
});

test('shared Codex prompt composer owns the quick reply menu for both prompt surfaces', () => {
  const composerSource = readSource('../src/components/CodexPromptComposer.vue');
  const composableSource = readSource('../src/composables/useQuickCommands.ts');
  const serviceSource = readSource('../src/services/quickCommands.ts');
  const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
  const detailSource = readSource('../src/pages/SessionDetailPage.vue');

  assert.match(composerSource, /:label="compact \? undefined : '快捷回复'"/);
  assert.match(composerSource, /:aria-label="compact \? '快捷回复' : undefined"/);
  assert.match(composerSource, /<q-tooltip v-if="compact">快捷回复<\/q-tooltip>/);
  assert.match(
    composerSource,
    /:class="compact \? 'quick-reply-btn app-icon-btn' : 'quick-reply-btn app-command-btn'"/,
  );
  assert.match(composerSource, /appendQuickCommand/);
  assert.match(composerSource, /command\.content/);
  assert.match(composableSource, /listQuickCommands/);
  assert.match(composableSource, /createQuickCommand/);
  assert.match(composableSource, /removeQuickCommand/);
  assert.doesNotMatch(composableSource, /localStorage|normalizeQuickCommands|已存在/);
  assert.match(serviceSource, /query QuickCommands/);
  assert.match(serviceSource, /pageInfo/);
  assert.match(serviceSource, /mutation CreateQuickCommand/);
  assert.match(serviceSource, /mutation DeleteQuickCommand/);
  assert.match(composableSource, /quickCommandsError/);
  assert.match(composableSource, /mutationVersion/);
  assert.match(newSessionSource, /<CodexPromptComposer/);
  assert.match(detailSource, /<CodexPromptComposer/);
});
