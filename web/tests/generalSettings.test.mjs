import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('global concurrency is database-backed and editable in general settings', () => {
  const serviceSource = readSource('../src/services/generalSettings.ts');
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
  const configSource = readSource('../../internal/infra/config/config.go');

  assert.match(serviceSource, /query GeneralSettings/);
  assert.match(serviceSource, /mutation UpdateGeneralSettings/);
  assert.match(settingsSource, /name="general"/);
  assert.match(settingsSource, /Agent 并发数量/);
  assert.match(settingsSource, /general\.agentMaxConcurrent/);
  assert.match(serviceSource, /agentWritableRoots/);
  assert.match(settingsSource, /Agent 目录白名单/);
  assert.match(settingsSource, /agentWritableRootsText/);
  assert.match(settingsSource, /每行必须是绝对路径/);
  assert.match(serviceSource, /sendShortcut: SendShortcut/);
  assert.match(settingsSource, /发送快捷键/);
  assert.match(settingsSource, /v-model="general\.sendShortcut"/);
  assert.match(settingsSource, /\{ label: 'Enter', value: 'enter' \}/);
  assert.match(settingsSource, /\{ label: 'Shift\+Enter', value: 'shift_enter' \}/);
  assert.match(settingsSource, /class="general-settings-group general-thinking-settings"/);
  assert.match(settingsSource, /<q-toggle\s+v-model="thinkingPhrasesEnabled"/);
  assert.match(
    settingsSource,
    /<q-slide-transition>[\s\S]*?v-if="thinkingPhrasesEnabled"[\s\S]*?思考语句类型/,
  );
  assert.match(settingsSource, /思考语句类型/);
  assert.match(settingsSource, /v-model="thinkingPhraseStyle"/);
  assert.match(settingsSource, /:options="sessionThinkingPhraseStyleOptions"/);
  assert.doesNotMatch(configSource, /ANYCODE_AGENT_MAX_CONCURRENT/);
});

test('global setting panels rely on navigation labels instead of repeated headings', () => {
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
  const quickCommandsSource = readSource('../src/components/QuickCommandManager.vue');

  assert.doesNotMatch(settingsSource, /class="global-settings-panel__header"/);
  assert.match(settingsSource, /<QuickCommandManager v-if="modelValue" hide-header \/>/);
  assert.match(quickCommandsSource, /v-if="!hideHeader" class="global-settings-panel__header"/);
});

test('general settings save valid changes after a debounce without a separating save button', () => {
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');

  assert.doesNotMatch(settingsSource, /label="保存常规设置"/);
  assert.match(settingsSource, /const generalSaveDebounceMs = 500/);
  assert.match(
    settingsSource,
    /watch\(\s*\[[\s\S]*?general\.value\.agentMaxConcurrent[\s\S]*?general\.value\.sendShortcut[\s\S]*?agentWritableRootsText[\s\S]*?scheduleGeneralSettingsSave/,
  );
  assert.match(
    settingsSource,
    /function scheduleGeneralSettingsSave\(\)[\s\S]*?generalSettingsValid\.value[\s\S]*?setTimeout\([\s\S]*?saveGeneralSettings\(\)/,
  );
  assert.match(settingsSource, /onBeforeUnmount\([\s\S]*?saveGeneralSettings\(\)/);
});

test('prompt composer dynamically applies the selected send shortcut', () => {
  const composerSource = readSource('../src/components/PromptComposer.vue');
  const invalidationSource = readSource('../src/composables/useGeneralSettingsInvalidation.ts');

  assert.doesNotMatch(composerSource, /@keydown\.shift\.enter/);
  assert.match(composerSource, /sendShortcut\.value === 'enter' && !event\.shiftKey/);
  assert.match(composerSource, /sendShortcut\.value === 'shift_enter' && event\.shiftKey/);
  assert.match(composerSource, /event\.preventDefault\(\);\s*emit\('submit'\)/);
  assert.match(invalidationSource, /sendShortcut: readonly\(sendShortcut\)/);
  assert.match(invalidationSource, /getGeneralSettings\(\)/);
});
