import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const storageKey = 'anycode.session-thinking-phrases.v1';

test('thinking phrase preferences share and persist the enabled state and selected style', () => {
  const { preferences, storage, source } = loadThinkingPhraseModule();
  const firstConsumer = preferences.useSessionThinkingPhrases();
  const secondConsumer = preferences.useSessionThinkingPhrases();

  assert.deepEqual(
    preferences.sessionThinkingPhraseStyleOptions.map((option) => option.label),
    ['普通', '中二', '疯狂', '女仆', '颜文字', '滑稽 Emoji'],
  );
  assert.equal(firstConsumer.thinkingPhrasesEnabled.value, false);
  assert.equal(firstConsumer.thinkingPhraseStyle.value, 'normal');
  assert.equal(firstConsumer.thinkingPhrases.value.length, 20);

  firstConsumer.thinkingPhrasesEnabled.value = true;
  firstConsumer.thinkingPhraseStyle.value = 'maid';

  assert.deepEqual(JSON.parse(storage.get(storageKey)), { enabled: true, style: 'maid' });
  assert.equal(secondConsumer.thinkingPhrasesEnabled.value, true);
  assert.equal(secondConsumer.thinkingPhraseStyle.value, 'maid');
  assert.equal(secondConsumer.thinkingPhrases.value.length, 20);
  assert.match(secondConsumer.thinkingPhrases.value[0], /主人/);
  assert.equal((source.match(/^\s{4}'[^']+',$/gm) ?? []).length, 120);

  const reloaded = loadThinkingPhraseModule(Object.fromEntries(storage));
  assert.equal(reloaded.preferences.useSessionThinkingPhrases().thinkingPhrasesEnabled.value, true);
  assert.equal(reloaded.preferences.useSessionThinkingPhrases().thinkingPhraseStyle.value, 'maid');
});

test('kaomoji and funny emoji styles each expose twenty phrases', () => {
  const { preferences } = loadThinkingPhraseModule();
  const consumer = preferences.useSessionThinkingPhrases();

  consumer.thinkingPhraseStyle.value = 'kaomoji';
  assert.equal(consumer.thinkingPhrases.value.length, 20);
  assert.match(consumer.thinkingPhrases.value[0], /[()]/);

  consumer.thinkingPhraseStyle.value = 'funny_emoji';
  assert.equal(consumer.thinkingPhrases.value.length, 20);
  assert.match(consumer.thinkingPhrases.value[0], /🤔/u);
});

test('invalid stored thinking phrase preferences fall back to disabled normal', () => {
  const { preferences } = loadThinkingPhraseModule({
    [storageKey]: JSON.stringify({ enabled: 'yes', style: 'unknown' }),
  });

  assert.deepEqual(preferences.readSessionThinkingPhrasePreferences(), {
    enabled: false,
    style: 'normal',
  });
  const consumer = preferences.useSessionThinkingPhrases();
  assert.equal(consumer.thinkingPhrasesEnabled.value, false);
  assert.equal(consumer.thinkingPhraseStyle.value, 'normal');
});

function loadThinkingPhraseModule(initial = {}) {
  const source = readFileSync(
    new URL('../src/composables/useSessionThinkingPhrases.ts', import.meta.url),
    'utf8',
  );
  const compiled = ts.transpileModule(source, {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  }).outputText;
  const storage = new Map(Object.entries(initial));
  globalThis.window = {
    localStorage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
  };
  const module = { exports: {} };
  new Function('require', 'module', 'exports', compiled)(
    createRequire(import.meta.url),
    module,
    module.exports,
  );
  return { preferences: module.exports, storage, source };
}
