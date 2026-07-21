import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const selectorSource = readSource('../src/components/CodexModelSelector.vue');
const controlsSource = readSource('../src/components/PromptConfigControls.vue');
const composerSource = readSource('../src/components/CodexPromptComposer.vue');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const serviceSource = readSource('../src/services/codexOptions.ts');
const preferencesSource = readSource('../src/services/newSessionPreferences.ts');

test('model and reasoning effort use separate lazy-loaded selectors', () => {
  assert.match(controlsSource, /<CodexModelSelector/);
  assert.match(controlsSource, /<CodexModelSelector[\s\S]*:disabled="disabled"/);
  assert.doesNotMatch(controlsSource, /class="compact-select model-select"/);
  assert.doesNotMatch(controlsSource, /class="compact-select effort-select"/);
  assert.match(selectorSource, /aria-label="Codex 模型"[\s\S]*class="compact-select model-select"/);
  assert.match(selectorSource, /aria-label="思考强度"[\s\S]*class="compact-select effort-select"/);
  assert.equal((selectorSource.match(/@popup-show="loadOptions"/g) ?? []).length, 2);
  assert.match(
    selectorSource,
    /function selectModel\(value: string\) \{\s*emit\('update:model', value\);\s*\}/,
  );
  assert.match(
    selectorSource,
    /function selectEffort\(value: string\) \{\s*emit\('update:effort', value\);\s*\}/,
  );
  assert.doesNotMatch(selectorSource, /onMounted/);
});

test('the catalog request is shared after success and can retry after failure', () => {
  assert.match(serviceSource, /let codexModelOptionsRequest: Promise<CodexModelOption\[]> \| null/);
  assert.match(serviceSource, /if \(!codexModelOptionsRequest\)/);
  assert.match(serviceSource, /\.catch\([\s\S]*codexModelOptionsRequest = null/);
});

test('composers only preload the catalog when the browser has no cached config', () => {
  assert.match(
    composerSource,
    /onMounted\(\(\) => \{[\s\S]*if \(!hasStoredSessionConfig\(\)\) void initializeCodexConfig\(\)/,
  );
  assert.match(
    preferencesSource,
    /window\.localStorage\.getItem\(NEW_SESSION_PREFERENCES_STORAGE_KEY\)/,
  );
  assert.match(newSessionSource, /loadNewSessionPreferences\(\)/);
});

test('new sessions cannot submit before a model and effort are ready', () => {
  assert.match(
    newSessionSource,
    /const codexConfigReady = computed\(\(\) => Boolean\(model\.value && effort\.value\)\)/,
  );
  assert.match(newSessionSource, /if \(!codexConfigReady\.value\)/);
  assert.match(newSessionSource, /!branchSelectionReady \|\| !codexConfigReady/);
});
