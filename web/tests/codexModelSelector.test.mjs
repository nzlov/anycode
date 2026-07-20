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
const sessionConfigCacheSource = readSource('../src/services/sessionConfigCache.ts');

test('model and reasoning effort use one lazy-loaded selector', () => {
  assert.match(controlsSource, /<CodexModelSelector/);
  assert.match(controlsSource, /<CodexModelSelector[\s\S]*:disabled="disabled"/);
  assert.doesNotMatch(controlsSource, /class="compact-select model-select"/);
  assert.doesNotMatch(controlsSource, /class="compact-select effort-select"/);
  assert.match(selectorSource, /@popup-show="loadOptions"/);
  assert.match(selectorSource, /aria-label="Codex 模型和思考强度"/);
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
    sessionConfigCacheSource,
    /window\.localStorage\.getItem\(SESSION_CONFIG_STORAGE_KEY\)/,
  );
  assert.match(newSessionSource, /loadStoredSessionConfig\(\)/);
});

test('new sessions cannot submit before a model and effort are ready', () => {
  assert.match(
    newSessionSource,
    /const codexConfigReady = computed\(\(\) => Boolean\(model\.value && effort\.value\)\)/,
  );
  assert.match(newSessionSource, /if \(!codexConfigReady\.value\)/);
  assert.match(newSessionSource, /!branchSelectionReady \|\| !codexConfigReady/);
});
