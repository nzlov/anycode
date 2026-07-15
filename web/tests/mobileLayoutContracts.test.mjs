import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  try {
    return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
  } catch {
    return '';
  }
}

const composerSource = readSource('../src/components/PromptComposer.vue');
const configControlsSource = readSource('../src/components/PromptConfigControls.vue');
const paginationSource = readSource('../src/components/AppPagination.vue');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const answerUserSource = readSource('../src/components/AnswerUserDialog.vue');
const layoutSource = readSource('../src/layouts/MainLayout.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const detailSource = readSource('../src/pages/SessionDetailPage.vue');
const diffPageSource = readSource('../src/pages/DiffPage.vue');
const diffWorkspaceSource = readSource('../src/components/DiffWorkspace.vue');
const commitHistorySource = readSource('../src/pages/CommitHistoryPage.vue');
const headlessE2ESource = readSource('../../scripts/headless-e2e.mjs');
const stylesSource = readSource('../src/css/app.scss');
const baseStyles = stylesSource.slice(0, stylesSource.indexOf('@media'));
const mobileStyles = stylesSource.slice(stylesSource.indexOf('@media (max-width: 699px)'));
const smallStylesStart = stylesSource.indexOf('@media (max-width: 599.98px)');
const smallStyles = smallStylesStart >= 0 ? stylesSource.slice(smallStylesStart) : '';

test('prompt runtime controls have one shared component owner', () => {
  assert.match(configControlsSource, /defineProps<\{/);
  assert.match(configControlsSource, /update:model/);
  assert.match(configControlsSource, /update:effort/);
  assert.match(configControlsSource, /update:permission/);
  assert.match(composerSource, /import PromptConfigControls/);
  assert.match(composerSource, /class="prompt-config-menu"/);
  assert.match(composerSource, /v-if="!forceConfigMenu && \(!compact \|\| !\$q\.screen\.lt\.md\)"/);
  assert.doesNotMatch(composerSource, /<q-select/);
});

test('new session dialog uses Quasar mobile maximization and one scrolling body', () => {
  assert.match(newSessionSource, /:maximized="!panel && \$q\.screen\.lt\.sm"/);
  assert.match(newSessionSource, /const \$q = useQuasar\(\)/);
  assert.match(newSessionSource, /class="new-session-dialog app-content-dialog"/);
  assert.match(
    stylesSource,
    /\.new-session-dialog\s*{[^}]*display:\s*flex[^}]*flex-direction:\s*column/s,
  );
  assert.match(stylesSource, /\.new-session-body\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(
    smallStyles,
    /\.app-content-dialog\s*{[^}]*width:\s*100vw\s*!important[^}]*height:\s*100dvh/s,
  );
});

test('answer user dialog uses Quasar mobile maximization without viewport clipping', () => {
  assert.match(answerUserSource, /:maximized="\$q\.screen\.lt\.sm"/);
  assert.match(answerUserSource, /class="answer-dialog app-content-dialog"/);
  assert.match(
    smallStyles,
    /\.app-content-dialog\s*{[^}]*height:\s*100dvh\s*!important[^}]*max-height:\s*100dvh\s*!important/s,
  );
  assert.doesNotMatch(answerUserSource, /@media \(max-width:\s*699px\)/);
});

test('headless approval fixture includes the required workflow node position', () => {
  assert.match(
    headlessE2ESource,
    /id:\s*'approve',[\s\S]*?position:\s*\{\s*x:\s*\d+,\s*y:\s*\d+\s*\}/,
  );
});

test('mobile command controls share a 44px touch contract', () => {
  assert.match(
    stylesSource,
    /\.app-icon-btn\s*{[^}]*min-width:\s*44px[^}]*min-height:\s*44px[^}]*touch-action:\s*manipulation/s,
  );
  assert.match(stylesSource, /\.app-command-btn\s*{[^}]*min-height:\s*44px/s);
  assert.match(stylesSource, /\.app-touch-list\s+\.q-item\s*{[^}]*min-height:\s*44px/s);
  assert.match(stylesSource, /\.prompt-config-controls \.q-field__control[^}]*min-height:\s*44px/s);
  assert.doesNotMatch(stylesSource, /\.new-session-grid \.q-btn-toggle/);
  assert.match(layoutSource, /aria-label="更多操作"/);
  assert.match(layoutSource, /class="app-icon-btn"/);
  assert.doesNotMatch(
    layoutSource,
    /aria-label="更多操作"[\s\S]{0,180}<q-tooltip>更多操作<\/q-tooltip>/,
  );
  assert.doesNotMatch(
    layoutSource,
    /aria-label="项目设置"[\s\S]{0,180}<q-tooltip>项目设置<\/q-tooltip>/,
  );
  assert.doesNotMatch(
    composerSource,
    /aria-label="运行参数"[\s\S]{0,180}<q-tooltip>运行参数<\/q-tooltip>/,
  );
  assert.doesNotMatch(layoutSource, /icon="palette"\s+aria-label="主题模式"/);
  assert.match(indexSource, /class="lane-icon-btn app-icon-btn"/);
  assert.doesNotMatch(
    stylesSource,
    /\.lane-icon-btn\s*{[^}]*(?:width|height|min-width|min-height):/s,
  );
  assert.doesNotMatch(`${layoutSource}\n${indexSource}`, /<q-list\s+dense(?![^>]*app-touch-list)/);
  assert.match(stylesSource, /\.toolbar-file-picker\s*{[^}]*width:\s*44px[^}]*max-width:\s*44px/s);
  assert.match(
    stylesSource,
    /\.toolbar-file-picker\.q-field--dense \.q-field__control[^}]*min-height:\s*44px[^}]*height:\s*44px/s,
  );
  assert.match(composerSource, /class="app-icon-btn"[^>]*aria-label="关闭预览"/s);
  assert.doesNotMatch(
    detailSource,
    /\.detail-composer__primary-btn\s*{[^}]*(?:width|height|min-width|min-height):\s*42px/s,
  );
});

test('mobile overview keeps the compact two-column metadata shape', () => {
  assert.match(
    stylesSource,
    /\.overview-card-meta-grid\s*{[^}]*repeat\(2,\s*minmax\(0,\s*1fr\)\)/s,
  );
  assert.doesNotMatch(
    mobileStyles,
    /\.overview-card-meta-grid\s*{[^}]*grid-template-columns:\s*1fr/s,
  );
  assert.match(baseStyles, /\.page-shell\.workbench-page\s*{[^}]*padding-bottom:\s*88px/s);
  assert.match(mobileStyles, /\.overview-card-footer\s*{[^}]*flex-wrap:\s*wrap/s);
  assert.doesNotMatch(mobileStyles, /\.overview-card-footer\s*{[^}]*padding-right:/s);
  assert.match(mobileStyles, /\.overview-card-actions\s*{[^}]*flex-wrap:\s*wrap/s);
});

test('pagination has one responsive component owner on every affected page', () => {
  assert.match(paginationSource, /:max-pages="\$q\.screen\.lt\.sm \? 3 : 5"/);
  assert.match(paginationSource, /:boundary-numbers="!\$q\.screen\.lt\.sm"/);
  assert.match(paginationSource, /class="app-pagination"/);
  assert.match(paginationSource, /size="24px"/);
  assert.match(
    stylesSource,
    /\.app-pagination \.q-btn\s*{[^}]*min-width:\s*44px\s*!important[^}]*min-height:\s*44px\s*!important/s,
  );
  for (const pageSource of [diffWorkspaceSource, commitHistorySource]) {
    assert.match(pageSource, /import AppPagination/);
    assert.match(pageSource, /<AppPagination/);
    assert.doesNotMatch(pageSource, /<q-pagination/);
  }
  assert.match(diffPageSource, /<DiffWorkspace/);
  assert.doesNotMatch(diffPageSource, /import AppPagination|<AppPagination/);
});

test('desktop diff page keeps the file list visible while diff content scrolls', () => {
  assert.match(
    diffWorkspaceSource,
    /@media \(min-width:\s*1024px\)[\s\S]*?\.diff-workspace__layout\s*{[^}]*grid-template-rows:\s*minmax\(0,\s*1fr\)[^}]*overflow-y:\s*auto[^}]*}[\s\S]*?\.diff-files\s*{[^}]*position:\s*sticky[^}]*top:\s*0[^}]*height:\s*100%[^}]*overflow-y:\s*auto/s,
  );
  assert.match(
    diffWorkspaceSource,
    /@container \(max-width:\s*1023px\)[\s\S]*?\.diff-files\s*{[^}]*position:\s*static[^}]*overflow-y:\s*visible/s,
  );
  assert.match(diffPageSource, /height:\s*calc\(100dvh\s*-\s*150px\)/);
  assert.doesNotMatch(diffPageSource, /position:\s*sticky/);
});

test('session detail mobile navigation shows one scroll owner at a time', () => {
  assert.match(detailSource, /class="detail-mobile-tabs lt-md"/);
  assert.match(detailSource, /q-tab name="session"/);
  assert.match(detailSource, /q-tab name="info"/);
  assert.match(detailSource, /q-tab name="changes"/);
  assert.match(detailSource, /q-tab name="artifacts"/);
  assert.match(
    detailSource,
    /const detailView = ref<'session' \| 'info' \| 'changes' \| 'artifacts'>\('session'\)/,
  );
  assert.match(detailSource, /const rightPanelTab = computed/);
  assert.match(detailSource, /GLUE: mobile detail navigation/);
  assert.match(detailSource, /\.detail-mobile-tabs\s*{[^}]*flex:\s*0\s+0\s+auto/s);
  assert.match(
    detailSource,
    /@media \(max-width:\s*1023\.98px\)[\s\S]*?\.detail-page\s*{[^}]*overflow:\s*hidden/s,
  );
  assert.match(
    stylesSource,
    /@media \(max-width:\s*1023\.98px\)[\s\S]*?\.detail-grid,[\s\S]*?grid-template-columns:\s*1fr/s,
  );
  assert.match(
    detailSource,
    /@media \(max-width:\s*1023\.98px\)[\s\S]*?\.right-panel--mobile-hidden\s*{[^}]*display:\s*none/s,
  );
  assert.doesNotMatch(
    detailSource,
    /@media \(max-width:\s*699px\)[\s\S]*?\.detail-page\s*{[^}]*overflow:\s*auto/s,
  );
});
