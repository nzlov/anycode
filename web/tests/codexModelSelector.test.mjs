import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const selectorSource = readSource('../src/components/CodexModelSelector.vue');
const controlsSource = readSource('../src/components/PromptConfigControls.vue');
const composerSource = readSource('../src/components/CodexPromptComposer.vue');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const serviceSource = readSource('../src/services/codexOptions.ts');
const preferencesSource = readSource('../src/services/newSessionPreferences.ts');

test('custom model selection validates and trims names without changing effort', () => {
  const events = [];
  const props = { model: 'known-model', effort: 'high', disabled: false };
  let dialogOptions;
  let confirm;
  const source = selectorSource.match(/function selectModel\([\s\S]*?\n\}/)[0];
  const selectModel = runInNewContext(`${stripTypeScriptTypes(source)}; selectModel`, {
    props,
    emit: (...args) => events.push(args),
    $q: {
      dialog: (options) => {
        dialogOptions = options;
        return { onOk: (callback) => { confirm = callback; } };
      },
    },
  });

  selectModel(null);
  assert.deepEqual(events, []);
  assert.equal(dialogOptions.prompt.model, 'known-model');
  assert.equal(dialogOptions.prompt.isValid('   '), false);
  assert.equal(dialogOptions.prompt.isValid('gpt-6.1-terra'), true);
  confirm('  gpt-6.1-terra  ');
  assert.deepEqual(events, [['update:model', 'gpt-6.1-terra']]);
  confirm('   ');
  props.disabled = true;
  confirm('another-model');
  assert.equal(events.length, 1);
  props.disabled = false;
  selectModel('known-model');
  assert.deepEqual(events[1], ['update:model', 'known-model']);
});

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
