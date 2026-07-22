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
const detailSource = readSource('../src/components/SessionDetailView.vue');
const diffPageSource = readSource('../src/pages/DiffPage.vue');
const diffWorkspaceSource = readSource('../src/components/DiffWorkspace.vue');
const diffViewerSource = readSource('../src/components/DiffViewer.vue');
const commitHistorySource = readSource('../src/pages/CommitHistoryPage.vue');
const routesSource = readSource('../src/router/routes.ts');
const newSessionPageSource = readSource('../src/pages/NewSessionPage.vue');
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

test('new session mobile entry uses a page and keeps one scrolling body', () => {
  assert.doesNotMatch(newSessionSource, /:maximized=/);
  assert.match(newSessionSource, /page\?: boolean/);
  assert.match(newSessionPageSource, /<NewSessionDialog[\s\S]*page/);
  assert.match(routesSource, /name: 'new-session'/);
  assert.match(newSessionSource, /const \$q = useQuasar\(\)/);
  assert.match(newSessionSource, /class="new-session-dialog app-content-dialog"/);
  assert.match(
    stylesSource,
    /\.new-session-dialog\s*{[^}]*display:\s*flex[^}]*flex-direction:\s*column/s,
  );
  assert.match(stylesSource, /\.new-session-body\s*{[^}]*overflow-y:\s*auto/s);
  assert.match(
    smallStyles,
    /\.app-content-dialog\s*{[^}]*width:\s*calc\(100vw - 24px\)\s*!important[^}]*height:\s*auto/s,
  );
});

test('answer user mobile entry opens session detail without maximizing the desktop dialog', () => {
  assert.doesNotMatch(answerUserSource, /:maximized=/);
  assert.match(indexSource, /openAnswerDialog[\s\S]*\$q\.screen\.lt\.sm[\s\S]*name: 'session-detail'/);
  assert.match(answerUserSource, /class="answer-dialog app-content-dialog"/);
  assert.match(
    smallStyles,
    /\.app-content-dialog\s*{[^}]*height:\s*auto\s*!important[^}]*max-height:\s*calc\(100dvh - 24px\)\s*!important/s,
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
    /\.toolbar-file-picker\s*{[^}]*border:\s*0[^}]*background:\s*transparent/s,
  );
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

test('shared pages use compact desktop spacing and no mobile horizontal boundary', () => {
  assert.match(baseStyles, /\.page-shell\s*{[^}]*padding:\s*12px/s);
  assert.match(mobileStyles, /\.page-shell\s*{[^}]*padding:\s*16px 0/s);
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

test('shared pagination remains available outside the unpaginated diff workspace', () => {
  assert.match(paginationSource, /:max-pages="\$q\.screen\.lt\.sm \? 3 : 5"/);
  assert.match(paginationSource, /:boundary-numbers="!\$q\.screen\.lt\.sm"/);
  assert.match(paginationSource, /class="app-pagination"/);
  assert.match(paginationSource, /size="24px"/);
  assert.match(
    stylesSource,
    /\.app-pagination \.q-btn\s*{[^}]*min-width:\s*44px\s*!important[^}]*min-height:\s*44px\s*!important/s,
  );
  assert.match(commitHistorySource, /import AppPagination/);
  assert.match(commitHistorySource, /<AppPagination/);
  assert.doesNotMatch(commitHistorySource, /<q-pagination/);
  assert.doesNotMatch(diffWorkspaceSource, /import AppPagination|<AppPagination|modelValue\.page/);
  assert.match(diffPageSource, /<DiffWorkspace/);
  assert.doesNotMatch(diffPageSource, /import AppPagination|<AppPagination/);
});

test('diff workspace keeps file navigation fixed while content scrolls with sticky file titles', () => {
  assert.match(
    diffViewerSource,
    /\.diff-viewer\s*{[^}]*min-width:\s*0[^}]*align-content:\s*start/s,
  );
  assert.match(
    diffWorkspaceSource,
    /\.diff-workspace__layout\s*{[^}]*grid-template-columns:\s*320px\s+minmax\(0,\s*1fr\)[^}]*grid-template-rows:\s*minmax\(0,\s*1fr\)/s,
  );
  assert.match(
    diffWorkspaceSource,
    /\.diff-content\s*{[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s,
  );
  assert.match(
    diffWorkspaceSource,
    /\.diff-files\s*{[^}]*height:\s*100%[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s,
  );
  assert.match(
    diffWorkspaceSource,
    /@container \(max-width:\s*1023px\)[\s\S]*?grid-template-rows:\s*minmax\(120px,\s*35%\)\s+minmax\(0,\s*1fr\)/s,
  );
  assert.match(
    diffViewerSource,
    /\.diff-file-header\s*{[^}]*position:\s*sticky[^}]*top:\s*0[^}]*z-index:\s*1/s,
  );
  assert.match(
    diffViewerSource,
    /\.diff-file-card\s*{[^}]*min-width:\s*0[^}]*overflow:\s*visible/s,
  );
  assert.match(
    diffViewerSource,
    /@media \(max-width:\s*720px\)[\s\S]*?\.diff-file-header\s*{[^}]*flex-wrap:\s*wrap/s,
  );
  assert.match(diffPageSource, /height:\s*calc\(100dvh\s*-\s*98px\)/);
  assert.match(detailSource, /:show-file-navigation="false"/);
});

test('session detail mobile navigation shows one scroll owner at a time', () => {
  assert.match(detailSource, /v-if="isMobileLayout"[\s\S]*class="detail-mobile-tabs"/);
  assert(
    detailSource.indexOf('<q-splitter') <
      detailSource.indexOf('class="detail-mobile-tabs"'),
    'mobile detail navigation should follow the detail content',
  );
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
  assert.match(
    detailSource,
    /\.detail-mobile-tabs\s*{[^}]*flex:\s*0\s+0\s+auto[^}]*border:\s*0[^}]*border-radius:\s*0/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile\s*{[^}]*padding:\s*0[^}]*overflow:\s*hidden/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile \.stream-card,\s*\.detail-page--mobile \.right-panel-card\s*{[^}]*border:\s*0[^}]*border-radius:\s*0/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile \.detail-grid\s*{[^}]*width:\s*100%/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile \.detail-splitter\s*>\s*:deep\(\.detail-splitter__panel--mobile-hidden\)\s*{[^}]*display:\s*none/s,
  );
  assert.doesNotMatch(
    detailSource,
    /@media \(max-width:\s*699px\)[\s\S]*?\.detail-page\s*{[^}]*overflow:\s*auto/s,
  );
});

test('session detail desktop splitter keeps one persisted and accessible layout owner', () => {
  assert.match(detailSource, /<q-splitter[\s\S]*?class="detail-grid detail-splitter"/s);
  assert.match(detailSource, /<q-splitter[\s\S]*?reverse[\s\S]*?unit="px"/s);
  assert.match(detailSource, /:model-value="rightPanelWidth"/);
  assert.match(detailSource, /:limits="\[minRightPanelWidth, maxRightPanelWidth\]"/);
  assert.match(detailSource, /:disable="isMobileLayout"/);
  assert.match(detailSource, /<q-resize-observer\s+@resize="onDetailSplitterResize"/);
  assert.equal(detailSource.match(/class="event-panel"/g)?.length, 1);
  assert.equal(detailSource.match(/class="right-panel"/g)?.length, 1);
  assert.match(detailSource, /const defaultRightPanelWidth = 360/);
  assert.match(detailSource, /const minRightPanelWidth = 320/);
  assert.match(detailSource, /const minLeftPanelWidth = 480/);
  assert.match(detailSource, /const detailSplitterGap = 16/);
  assert.match(detailSource, /const splitterKeyboardStep = 16/);
  assert.match(
    detailSource,
    /const detailSplitterStorageKey = 'anycode:session-detail:right-panel-width'/,
  );
  assert.match(detailSource, /window\.localStorage\.getItem\(detailSplitterStorageKey\)/);
  assert.match(detailSource, /window\.localStorage\.setItem\(/);
  assert.match(
    detailSource,
    /if \(raw === null \|\| raw\.trim\(\) === ''\) return defaultRightPanelWidth/,
  );
  assert.match(
    detailSource,
    /Number\.isFinite\(value\)\s*\?\s*Math\.max\(minRightPanelWidth, Math\.round\(value\)\)/,
  );
  assert.match(detailSource, /const rightPanelWidth = computed/);
  assert.match(
    detailSource,
    /Math\.min\(preferredRightPanelWidth\.value, maxRightPanelWidth\.value\)/,
  );
  assert.match(detailSource, /#separator/);
  assert.match(detailSource, /role="separator"/);
  assert.match(detailSource, /aria-orientation="vertical"/);
  assert.match(detailSource, /:aria-valuemin="minRightPanelWidth"/);
  assert.match(detailSource, /:aria-valuemax="maxRightPanelWidth"/);
  assert.match(detailSource, /:aria-valuenow="rightPanelWidth"/);
  assert.match(detailSource, /@keydown\.left\.prevent="resizeRightPanel\(splitterKeyboardStep\)"/);
  assert.match(
    detailSource,
    /@keydown\.right\.prevent="resizeRightPanel\(-splitterKeyboardStep\)"/,
  );
  assert.match(
    detailSource,
    /\.detail-splitter\s*>\s*:deep\(\.q-splitter__panel\)\s*{[^}]*overflow:\s*hidden/s,
  );
  assert.match(
    detailSource,
    /\.right-panel-card\s+:deep\(\.q-tab-panels\)\s*{[^}]*overflow:\s*auto/s,
  );
  assert.doesNotMatch(
    detailSource,
    /\.right-panel-card\s+:deep\(\.q-tab-panels\)\s*{[^}]*overflow-x:\s*hidden/s,
  );
  assert.match(
    detailSource,
    /\.append-history\s*{[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)/s,
  );
  assert.match(
    detailSource,
    /\.append-history\s+:deep\(\.q-item__section--main\)\s*{[^}]*flex-wrap:\s*nowrap/s,
  );
  assert.match(
    detailSource,
    /\.append-history__attachments\s+:deep\(\.q-chip\)\s*{[^}]*margin:\s*0[^}]*max-width:\s*100%/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile \.detail-splitter\s*>\s*:deep\(\.q-splitter__separator\)\s*{[^}]*display:\s*none/s,
  );
  assert.match(
    detailSource,
    /\.detail-page--mobile \.detail-splitter\s*>\s*:deep\(\.q-splitter__panel\)\s*{[^}]*width:\s*100%\s*!important/s,
  );
});
