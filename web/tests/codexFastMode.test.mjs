import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const controlsSource = readSource('../src/components/PromptConfigControls.vue');
const composerSource = readSource('../src/components/PromptComposer.vue');
const codexComposerSource = readSource('../src/components/CodexPromptComposer.vue');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const detailSource = readSource('../src/pages/SessionDetailPage.vue');
const sessionsSource = readSource('../src/services/sessions.ts');
const indexSource = readSource('../src/pages/IndexPage.vue');
const preferencesSource = readSource('../src/services/newSessionPreferences.ts');

test('Fast uses the shared runtime controls on desktop and mobile prompt surfaces', () => {
  assert.match(controlsSource, /<q-checkbox[\s\S]*v-model="fastModel"[\s\S]*label="Fast"/);
  assert.match(controlsSource, /:disable="disabled \|\| readonlyConfig"/);
  assert.match(controlsSource, /fast: boolean/);
  assert.match(controlsSource, /'update:fast'/);

  for (const source of [composerSource, codexComposerSource]) {
    assert.match(source, /:fast="fast"/);
    assert.match(source, /@update:fast="emit\('update:fast', \$event\)"/);
  }
  assert.match(composerSource, /class="prompt-config-menu"/);
  assert.match(newSessionSource, /v-model:fast="fast"/);
  assert.match(detailSource, /v-model:fast="composerFast"/);
});

test('new sessions restore and immediately persist one frontend preference record', () => {
  assert.doesNotMatch(sessionsSource, /LastSessionConfig|lastSessionConfig/);
  assert.match(
    preferencesSource,
    /NEW_SESSION_PREFERENCES_STORAGE_KEY = 'anycode\.newSessionPreferences'/,
  );
  assert.match(
    preferencesSource,
    /window\.localStorage\.getItem\(NEW_SESSION_PREFERENCES_STORAGE_KEY\)/,
  );
  assert.match(newSessionSource, /const cachedPreferences = loadNewSessionPreferences\(\)/);
  assert.match(newSessionSource, /const projectId = ref\(cachedPreferences\?\.projectId \?\? ''\)/);
  assert.match(newSessionSource, /const branch = ref\(cachedPreferences\?\.baseBranch \?\? ''\)/);
  assert.match(
    newSessionSource,
    /const priority = ref<SessionPriority>\(cachedPreferences\?\.priority \?\? 'medium'\)/,
  );
  assert.match(newSessionSource, /const model = ref\(storedRunConfig\.codexModel\)/);
  assert.match(newSessionSource, /const effort = ref\(storedRunConfig\.reasoningEffort\)/);
  assert.match(newSessionSource, /const permission = ref\(storedRunConfig\.permissionMode\)/);
  assert.match(newSessionSource, /const fast = ref\(storedRunConfig\.fastMode\)/);
  assert.match(
    preferencesSource,
    /function storeNewSessionPreferences\(preferences: NewSessionPreferences\)[\s\S]*window\.localStorage\.setItem\([\s\S]*NEW_SESSION_PREFERENCES_STORAGE_KEY,[\s\S]*JSON\.stringify\(preferences\)/,
  );
  assert.match(
    newSessionSource,
    /watch\(\s*\[projectId, branch, model, effort, permission, fast, priority\],[\s\S]*storeNewSessionPreferences\(\{[\s\S]*projectId: selectedProjectId,[\s\S]*baseBranch,[\s\S]*codexModel,[\s\S]*reasoningEffort,[\s\S]*permissionMode,[\s\S]*fastMode,[\s\S]*priority: value/,
  );
  assert.doesNotMatch(newSessionSource, /rememberProjectId|storeSessionConfig/);
  assert.doesNotMatch(
    newSessionSource,
    /getLastSessionConfig|loadLastConfigForProject|lastConfigRequestToken|runConfigLoading|runConfigError/,
  );
  assert.match(
    preferencesSource,
    /fastMode: typeof record\.fastMode === 'boolean' \? record\.fastMode : false/,
  );
});

test('Fast is submitted and persisted without changing the running process immediately', () => {
  assert.match(newSessionSource, /fastMode: fast\.value/);
  assert.match(detailSource, /current\.config\.fastMode !== composerFast\.value/);
  assert.match(detailSource, /fastMode: composerFast\.value/);
  assert.match(detailSource, /composerFast\.value = value\.config\.fastMode/);
  assert.doesNotMatch(detailSource, /watch\(\s*composerFast/);
});

test('Fast is part of session GraphQL config but not card or info badges', () => {
  assert.match(sessionsSource, /export interface SessionConfig \{[\s\S]*fastMode: boolean/);
  assert.match(sessionsSource, /config \{[\s\S]*permissionMode[\s\S]*fastMode/);
  assert.doesNotMatch(indexSource, /fastMode|Fast 模式/);
  assert.doesNotMatch(detailSource, /<q-item-label caption>Fast/);
  assert.doesNotMatch(detailSource, /<q-badge[^>]*label="Fast"/);
});
