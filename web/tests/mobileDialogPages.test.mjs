import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const routes = readSource('../src/router/routes.ts');
const layout = readSource('../src/layouts/MainLayout.vue');
const overview = readSource('../src/pages/IndexPage.vue');
const detail = readSource('../src/components/SessionDetailView.vue');
const artifacts = readSource('../src/components/SessionArtifactsPanel.vue');
const settings = readSource('../src/components/GlobalSettingsDialog.vue');
const promptPanel = readSource('../src/components/PromptAppendEditPanel.vue');
const promptPage = readSource('../src/pages/PromptAppendEditPage.vue');

test('every persisted mobile content surface has a direct route', () => {
  for (const routeName of [
    'new-session',
    'settings',
    'project-create',
    'project-settings',
    'session-artifacts',
    'session-artifact',
    'prompt-append-edit',
  ]) {
    assert.match(routes, new RegExp(`name: '${routeName}'`));
  }
});

test('mobile entry handlers navigate while desktop dialog state remains available', () => {
  assert.match(layout, /function openNewSession[\s\S]*\$q\.screen\.lt\.sm[\s\S]*newSessionOpen\.value = true/);
  assert.match(layout, /function openSettings[\s\S]*\$q\.screen\.lt\.sm[\s\S]*settingsDialogOpen\.value = true/);
  assert.match(settings, /function openProjectDirectory[\s\S]*name: 'project-create'/);
  assert.match(settings, /function openProjectSettings[\s\S]*name: 'project-settings'/);
  assert.match(overview, /function openDiffDialog[\s\S]*name|path: '\/diff'/);
  assert.match(overview, /function openArtifactDialog[\s\S]*name: 'session-artifacts'/);
  assert.match(detail, /function openPromptAppendEditor[\s\S]*name: 'prompt-append-edit'/);
  assert.match(artifacts, /function openPreview[\s\S]*name: 'session-artifact'/);
});

test('prompt edit page and desktop dialog share one content component', () => {
  assert.match(promptPage, /<PromptAppendEditPanel/);
  assert.match(detail, /<PromptAppendEditPanel/);
  assert.match(promptPanel, /class="prompt-edit-dialog app-content-dialog"/);
  assert.doesNotMatch([layout, overview, detail, artifacts, settings].join('\n'), /:maximized=/);
});
