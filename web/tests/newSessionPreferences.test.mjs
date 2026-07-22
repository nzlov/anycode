import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

const storageKey = 'anycode.newSessionPreferences';

test('new-session preferences round-trip every retained selection', () => {
  const { preferences, storage } = loadPreferencesModule();
  const selected = {
    projectId: 'project-2',
    baseBranch: 'release/next',
    codexModel: 'gpt-5.4',
    reasoningEffort: 'high',
    permissionMode: 'workspace-write',
    fastMode: true,
    priority: 'high',
  };

  preferences.storeNewSessionPreferences(selected);

  assert.deepEqual(JSON.parse(storage.get(storageKey)), selected);
  assert.deepEqual(preferences.loadNewSessionPreferences(), selected);
  assert.equal(preferences.hasStoredSessionConfig(), true);
});

test('legacy runtime config and project selection migrate into one preference record', () => {
  const { preferences } = loadPreferencesModule({
    'anycode.lastSessionConfig': JSON.stringify({
      codexModel: 'gpt-5.3-codex',
      reasoningEffort: 'medium',
      permissionMode: 'read-only',
      fastMode: true,
    }),
    'anycode.lastNewSessionProjectId': 'project-1',
  });

  assert.deepEqual(preferences.loadNewSessionPreferences(), {
    projectId: 'project-1',
    baseBranch: '',
    codexModel: 'gpt-5.3-codex',
    reasoningEffort: 'medium',
    permissionMode: 'read-only',
    fastMode: true,
    priority: 'medium',
  });
});

test('invalid cached fields fall back without leaking malformed values', () => {
  const { preferences } = loadPreferencesModule({
    [storageKey]: JSON.stringify({
      projectId: 7,
      baseBranch: null,
      codexModel: false,
      reasoningEffort: [],
      permissionMode: {},
      fastMode: 'yes',
      priority: 'urgent',
    }),
  });

  assert.deepEqual(preferences.loadNewSessionPreferences(), {
    projectId: '',
    baseBranch: '',
    codexModel: '',
    reasoningEffort: '',
    permissionMode: '',
    fastMode: false,
    priority: 'medium',
  });
  assert.equal(preferences.hasStoredSessionConfig(), false);
});

function loadPreferencesModule(initial = {}) {
  const source = readFileSync(
    new URL('../src/services/newSessionPreferences.ts', import.meta.url),
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
    () => {
      throw new Error('Unexpected runtime import');
    },
    module,
    module.exports,
  );
  return { preferences: module.exports, storage };
}
