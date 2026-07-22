import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layoutSource = readSource('../src/layouts/MainLayout.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const toolbarSource = readSource('../src/components/PageToolbar.vue');
const settingsSource = readSource('../src/components/GlobalSettingsDialog.vue');
const directorySource = readSource('../src/components/ProjectDirectoryDialog.vue');
const projectsComposableSource = readSource('../src/composables/useProjects.ts');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const stylesSource = readSource('../src/css/app.scss');

test('application shell removes the global drawer and duplicate project entry', () => {
  assert.doesNotMatch(layoutSource, /<q-drawer|leftDrawerOpen|toggleLeftDrawer|app-drawer/);
  assert.match(indexSource, /<PageToolbar title="AnyCode"/);
  assert.match(toolbarSource, /<q-toolbar-title/);
  assert.doesNotMatch(layoutSource, /aria-label="选择项目目录"/);
  assert.doesNotMatch(layoutSource, /activeProjectId|refreshActiveProject|projectActive/);
  assert.match(layoutSource, /icon="img:\/icons\/anycode\.svg"/);
  assert.match(layoutSource, /aria-label="返回总览"/);
  assert.match(layoutSource, /:to="\{ name: 'overview' \}"/);
  assert.match(layoutSource, /\{\{ sessionTitle \|\| '会话详情' \}\}/);
  assert.match(layoutSource, /@session-title="sessionTitle = \$event"/);
  assert.match(layoutSource, /id="app-page-toolbar"/);
  assert.match(layoutSource, /<GlobalSettingsDialog v-model="settingsDialogOpen"/);
});

test('overview uses project chips and a single icon-only history entry', () => {
  assert.match(indexSource, /class="overview-filter-toolbar"/);
  assert.match(indexSource, /v-for="project in projectChips"/);
  assert.match(indexSource, /:aria-pressed="isProjectVisible\(project\.id\)"/);
  assert.match(indexSource, /visibility_off/);
  assert.match(layoutSource, /icon="history"/);
  assert.match(layoutSource, /aria-label="历史卡片"/);
  assert.doesNotMatch(layoutSource, /\slabel="历史卡片"/);
  assert.doesNotMatch(indexSource, /title:\s*'最新'|title:\s*'历史'|pageTitle/);
  assert.doesNotMatch(indexSource, /uniqueHistoryCards|historyCards|hasMoreHistory/);
  assert.match(layoutSource, /scope:\s*'closed'/);
});

test('overview derives unique project chips by id and filters only cards', () => {
  assert.match(indexSource, /const projectChips = computed/);
  assert.match(indexSource, /seen\.has\(card\.projectId\)/);
  assert.match(indexSource, /seen\.add\(card\.projectId\)/);
  assert.match(indexSource, /id:\s*card\.projectId/);
  assert.match(indexSource, /name:\s*card\.projectName/);
  assert.match(indexSource, /const visibleLatestCards = computed/);
  assert.match(indexSource, /!hiddenProjectIds\.value\.has\(card\.projectId\)/);
  assert.match(indexSource, /v-for="card in visibleLatestCards"/);
  assert.match(indexSource, /当前没有显示的卡片/);
});

test('overview persists hidden project ids defensively in local storage', () => {
  assert.match(indexSource, /anycode\.overview\.hidden-projects\.v1/);
  assert.match(indexSource, /window\.localStorage\.getItem/);
  assert.match(indexSource, /JSON\.parse/);
  assert.match(indexSource, /Array\.isArray/);
  assert.match(indexSource, /catch/);
  assert.match(indexSource, /window\.localStorage\.setItem/);
  assert.match(indexSource, /projects\.value\.map\(\(project\) => project\.id\)/);
});

test('concurrent project consumers await the same list request before pruning filters', () => {
  assert.match(projectsComposableSource, /let loadPromise: Promise<void> \| null = null/);
  assert.match(projectsComposableSource, /if \(loadPromise\) return loadPromise/);
  assert.match(projectsComposableSource, /loadPromise = listProjects\(\)/);
  assert.match(indexSource, /await loadProjects\(\);[\s\S]*pruneHiddenProjectIds\(\)/);
});

test('project mutations invalidate older list snapshots and loading rows are not actionable', () => {
  assert.match(projectsComposableSource, /let mutationRevision = 0/);
  assert.match(projectsComposableSource, /const revision = mutationRevision/);
  assert.match(projectsComposableSource, /if \(revision !== mutationRevision\) return/);
  assert.equal((projectsComposableSource.match(/mutationRevision \+= 1/g) ?? []).length, 3);
  assert.match(settingsSource, /:disable="projectsLoading \|\| removingProject"/);
});

test('removing the last project clears the persistent new-session selection', () => {
  assert.match(
    newSessionSource,
    /if \(!nextProjectId\) \{[\s\S]*projectId\.value = '';[\s\S]*branch\.value = '';[\s\S]*return true;/,
  );
});

test('global settings owns complete project management', () => {
  assert.match(settingsSource, /name="projects"/);
  assert.match(settingsSource, /<project-directory-dialog/);
  assert.match(settingsSource, /<project-settings-dialog/);
  assert.match(settingsSource, /openProjectOverview\(project\.id\)/);
  assert.match(settingsSource, /openProjectSettings\(project\)/);
  assert.match(settingsSource, /openWorkflowConfig\(project\.id\)/);
  assert.match(settingsSource, /confirmRemoveProject\(project\.id, project\.name\)/);
  assert.match(settingsSource, /aria-label="新增项目"/);
  assert.match(settingsSource, /class="global-settings-tabs lt-sm"/);
  assert.match(stylesSource, /\.global-settings-tabs/);
});

test('an empty first visit requires choosing a project before showing the application shell', () => {
  assert.match(layoutSource, /<q-header v-if="applicationReady"/);
  assert.match(
    layoutSource,
    /<ProjectDirectoryDialog[\s\S]*:model-value="initialProjectRequired"[\s\S]*:persistent="initialProjectRequired"/,
  );
  assert.match(layoutSource, /projectsLoaded\.value && projects\.value\.length === 0/);
  assert.match(layoutSource, /void loadProjects\(\)[\s\S]*finally\(\(\) => \{/);
  assert.match(directorySource, /:persistent="persistent"/);
  assert.equal((directorySource.match(/v-if="!persistent"/g) ?? []).length, 2);
});

test('drawer removal realigns the persistent create panel', () => {
  assert.doesNotMatch(stylesSource, /left:\s*312px/);
  assert.match(
    stylesSource,
    /@media \(min-width:\s*700px\)[\s\S]*?\.new-session-dialog--panel\s*{[^}]*left:\s*24px/s,
  );
});
