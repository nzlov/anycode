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
const projects = readSource('../src/pages/ProjectsPage.vue');
const promptPanel = readSource('../src/components/PromptAppendEditPanel.vue');
const promptPage = readSource('../src/pages/PromptAppendEditPage.vue');

test('every persisted mobile content surface has a direct route', () => {
  for (const routeName of [
    'diff',
    'new-session',
    'settings',
    'projects',
    'project-create',
    'project-settings',
    'session-artifacts',
    'session-artifact',
    'prompt-append-edit',
  ]) {
    assert.match(routes, new RegExp(`name: '${routeName}'`));
  }
});

test('standalone mobile entries navigate while artifact previews open in place', () => {
  assert.match(
    overview,
    /function openNewSession[\s\S]*\$q\.screen\.lt\.sm[\s\S]*newSessionOpen\.value = true/,
  );
  assert.match(
    layout,
    /function openSettings[\s\S]*\$q\.screen\.lt\.sm[\s\S]*settingsDialogOpen\.value = true/,
  );
  assert.match(
    projects,
    /function openCreateProject[\s\S]*\$q\.screen\.lt\.sm[\s\S]*name: 'project-create'[\s\S]*createDialogOpen\.value = true/,
  );
  assert.match(projects, /function openProjectSettings[\s\S]*name: 'project-settings'/);
  assert.match(
    overview,
    /function openDiffDialog[\s\S]*!isDesktopOverview\.value[\s\S]*path: '\/diff'/,
  );
  assert.match(
    overview,
    /function openDiffDialog[\s\S]*!isDesktopOverview\.value[\s\S]*mode: 'single'/,
  );
  assert.match(
    overview,
    /function openArtifactDialog[\s\S]*!isDesktopOverview\.value[\s\S]*name: 'session-artifacts'/,
  );
  assert.match(detail, /function openPromptAppendEditor[\s\S]*name: 'prompt-append-edit'/);
  assert.match(
    artifacts,
    /function openPreview\(file[\s\S]*selected\.value = file[\s\S]*previewOpen\.value = true/,
  );
  assert.doesNotMatch(artifacts, /useRouter|name: 'session-artifact'/);
  assert.match(artifacts, /:maximized="\$q\.screen\.lt\.md"/);
  assert.match(detail, /<q-tab name="changes" icon="difference" label="变更"/);
  assert.match(detail, /<q-tab name="artifacts" icon="inventory_2" label="临时文件"/);
  assert.doesNotMatch(detail, /<q-route-tab|mobileDiffRoute|allArtifactsRoute/);
  assert.match(
    layout,
    /v-if="isContentRoute"[\s\S]*aria-label="返回上一页"[\s\S]*@click="goBackFromContent"/,
  );
});

test('prompt edit page and desktop dialog share one content component', () => {
  assert.match(promptPage, /<PromptAppendEditPanel/);
  assert.match(detail, /<PromptAppendEditPanel/);
  assert.match(promptPanel, /class="prompt-edit-dialog app-content-dialog"/);
  assert.doesNotMatch([layout, overview, settings].join('\n'), /:maximized=/);
  assert.match(detail, /:maximized="isMobileLayout"/);
});
