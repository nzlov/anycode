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

test('new sessions load project-scoped defaults without global runtime config storage', () => {
  assert.match(sessionsSource, /query LastSessionConfig\(\$projectId: ID!\)/);
  assert.match(sessionsSource, /lastSessionConfig\(projectId: \$projectId\)/);
  assert.match(newSessionSource, /getLastSessionConfig\(value\)/);
  assert.match(newSessionSource, /watch\(projectId,[\s\S]*loadLastConfigForProject\(value\)/);
  assert.match(
    newSessionSource,
    /lastConfigRequestToken\.value !== token \|\| projectId\.value !== value/,
  );
  assert.match(newSessionSource, /model\.value = config\.codexModel/);
  assert.match(newSessionSource, /fast\.value = config\.fastMode/);
  assert.match(newSessionSource, /function resetRunConfig\(\)[\s\S]*fast\.value = false/);
  assert.match(
    sessionsSource,
    /export async function getLastSessionConfig[\s\S]*?variables: \{ projectId \},\s*notify: false,\s*\}\);\s*return data\.lastSessionConfig/,
  );
  assert.match(newSessionSource, /v-if="runConfigError"[\s\S]*@click="retryLastConfig"/);
  assert.match(
    newSessionSource,
    /if \(runConfigLoading\.value \|\| runConfigError\.value\) return false/,
  );
  assert.match(
    newSessionSource,
    /catch \(error\)[\s\S]*runConfigError\.value = `获取项目运行参数失败/,
  );
  assert.doesNotMatch(
    newSessionSource,
    /anycode\.lastSessionConfig|rememberSessionConfig|storedSessionConfig/,
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
