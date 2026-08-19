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
  assert.match(settingsSource, /白名单目录/);
  assert.match(settingsSource, /agentWritableRoots/);
  assert.match(settingsSource, /请输入根目录以外的绝对路径/);
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

test('agent writable roots use a dedicated editable list with direct input and directory selection', () => {
  const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
  const directorySource = readSource('../src/components/ProjectDirectoryDialog.vue');
  const stylesSource = readSource('../src/css/app.scss');

  assert.match(settingsSource, /name="writable_roots"[^>]*label="白名单目录"/);
  assert.match(settingsSource, /activeSection === 'writable_roots'/);
  assert.match(settingsSource, /v-for="\(root, index\) in agentWritableRoots"/);
  assert.match(settingsSource, /aria-label="新增白名单目录"/);
  assert.match(settingsSource, /icon="edit"[\s\S]*startEditWritableRoot\(index\)/);
  assert.match(settingsSource, /icon="delete_outline"[\s\S]*removeWritableRoot\(index\)/);
  assert.match(settingsSource, /<WritableRootEditor\s+v-if="addingWritableRoot"/);
  assert.match(
    settingsSource,
    /v-if="editingWritableRootIndex === index"[\s\S]*<WritableRootEditor[\s\S]*inline/,
  );
  assert.doesNotMatch(
    settingsSource,
    /v-if="addingWritableRoot \|\| editingWritableRootIndex !== null"/,
  );
  assert.match(readSource('../src/components/WritableRootEditor.vue'), /icon="folder_open"/);
  assert.match(
    settingsSource,
    /<ProjectDirectoryDialog[\s\S]*select-only[\s\S]*@select="selectWritableRoot"/,
  );
  assert.doesNotMatch(settingsSource, /v-model="agentWritableRootsText"/);
  assert.match(directorySource, /selectOnly \? '选择目录' : '选择项目目录'/);
  assert.match(directorySource, /emit\('select', selected\.value\)/);
  assert.match(
    stylesSource,
    /\.global-settings-add-fab\s*\{[^}]*position:\s*relative;/s,
  );
  assert.match(
    stylesSource,
    /\.writable-root-editor\s*\{[^}]*display:\s*flex;[^}]*flex-wrap:\s*nowrap;[^}]*align-items:\s*flex-start;/s,
  );
  assert.match(
    stylesSource,
    /\.writable-root-editor\s*>\s*\.q-field\s*\{[^}]*min-width:\s*0;[^}]*flex:\s*1 1 auto;/s,
  );
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
    /watch\(\s*\[[\s\S]*?general\.value\.agentMaxConcurrent[\s\S]*?general\.value\.sendShortcut[\s\S]*?agentWritableRoots[\s\S]*?scheduleGeneralSettingsSave/,
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
