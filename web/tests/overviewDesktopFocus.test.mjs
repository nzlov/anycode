import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layoutSource = readSource('../src/layouts/MainLayout.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');
const stylesSource = readSource('../src/css/app.scss');

test('desktop card overview replaces the create FAB with the persistent create panel', () => {
  assert.match(indexSource, /<q-page-sticky v-if="!showDesktopFocusLayout"/);
  assert.match(indexSource, /:panel="showDesktopFocusLayout"/);
  assert.match(indexSource, /const overviewDesktopMinWidth = 700/);
  assert.match(indexSource, /const showDesktopFocusLayout = computed/);
  assert.match(indexSource, /!isHorizontalView\.value/);
  assert.doesNotMatch(layoutSource, /<new-session-dialog|NewSessionDialog/);

  assert.match(newSessionSource, /panel\?: boolean/);
  assert.match(newSessionSource, /:model-value="page \? undefined : dialogVisible"/);
  assert.match(newSessionSource, /:seamless="panel"/);
  assert.match(newSessionSource, /:no-focus="panel"/);
  assert.match(newSessionSource, /new-session-dialog--panel/);
  assert.match(newSessionSource, /const dialogVisible = computed/);
});

test('overview keeps only latest cards and links history from the application header', () => {
  assert.match(indexSource, /class="overview-filter-toolbar"/);
  assert.doesNotMatch(indexSource, /icon="history"|aria-label="历史卡片"|:to="sessionsRoute"/);
  assert.match(layoutSource, /icon="history"/);
  assert.match(layoutSource, /aria-label="历史卡片"/);
  assert.doesNotMatch(layoutSource, /\slabel="历史卡片"/);
  assert.match(layoutSource, /:to="sessionsRoute"/);
  assert.match(indexSource, /const showDesktopFocusLayout = computed/);
  assert.match(indexSource, /v-for="card in visibleLatestCards"/);
  assert.doesNotMatch(indexSource, /historySection|historyCards|hasMoreHistory/);
});

test('desktop create panel uses one project row and expands with prompt content', () => {
  assert.match(
    stylesSource,
    /:root\s*{[^}]*--overview-create-panel-height:\s*min\(320px, calc\(100dvh - 104px\)\)/s,
  );
  assert.match(
    stylesSource,
    /\.page-shell\.workbench-page--desktop-focus\s*{[^}]*padding-bottom:\s*calc\(var\(--overview-create-panel-height\) \+ 48px\)/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel\s*{[^}]*position:\s*fixed[^}]*bottom:\s*24px[^}]*height:\s*auto[^}]*max-height:\s*none\s*!important[^}]*z-index:/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-body\s*{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*visible/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-panel-host \.new-session-dialog--panel\s*{[^}]*overflow:\s*visible/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-grid\s*{[^}]*grid-template-columns:\s*minmax\(0, 1fr\)[^}]*grid-auto-flow:\s*column[^}]*gap:\s*5px/s,
  );
  assert.match(newSessionSource, /label="项目"[\s\S]*class="branch-picker"[\s\S]*label="优先级"/);
  assert.match(
    stylesSource,
    /@media \(min-width: 700px\)[\s\S]*?\.new-session-dialog--panel\s*{[^}]*left:\s*24px/s,
  );
});

test('desktop create panel presents the prompt before a compact context row', () => {
  assert.match(
    newSessionSource,
    /<q-card-section\s+v-if="!panel"\s+class="new-session-dialog__header/s,
  );
  assert.match(newSessionSource, /<q-separator v-if="!panel"/);
  assert.match(newSessionSource, /class="new-session-grid new-session-context"/);
  assert.match(newSessionSource, /:outlined="!panel"[\s\S]*?:borderless="panel"/);
  assert.match(newSessionSource, /:title="panel \? '' : '提示词'"/);
  assert.match(newSessionSource, /:show-badge="!panel"/);
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-body\s*{[^}]*gap:\s*0[^}]*padding:\s*0/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.prompt-shell\s*{[^}]*order:\s*1[^}]*border:\s*0/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-context\s*{[^}]*order:\s*2[^}]*display:\s*flex[^}]*flex-wrap:\s*wrap[^}]*gap:\s*5px/s,
  );
  assert.match(
    stylesSource,
    /@media \(min-width:\s*700px\)[\s\S]*?\.new-session-dialog--panel \.new-session-context \.q-field__control[^}]*min-height:\s*44px/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-context > \.q-field\s*{[^}]*width:\s*max-content[^}]*min-width:\s*44px[^}]*max-width:\s*24ch[^}]*flex:\s*0\s+1\s+auto/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-context \.branch-picker\s*{[^}]*width:\s*max-content[^}]*min-width:\s*44px[^}]*max-width:\s*calc\(24ch \+ 50px\)[^}]*flex:\s*0\s+1\s+auto/s,
  );
  assert.doesNotMatch(stylesSource, /\.branch-picker__select/);
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-context \.new-session-priority\s*{[^}]*width:\s*max-content[^}]*min-width:\s*44px[^}]*max-width:\s*12ch/s,
  );
  assert.doesNotMatch(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-context > \.q-field\s*{[^}]*flex:\s*0\s+1\s+240px/s,
  );
  assert.match(newSessionSource, /<q-tooltip>项目：\{\{ selectedProject\?\.name \}\}<\/q-tooltip>/);
  assert.match(newSessionSource, /<q-tooltip>基础分支：\{\{ branch \}\}<\/q-tooltip>/);
});

test('wide desktop create panel is centered at half the viewport width', () => {
  assert.match(
    stylesSource,
    /@media \(min-width:\s*1024px\)[\s\S]*?\.new-session-dialog--panel\s*{[^}]*left:\s*25vw[^}]*right:\s*25vw/s,
  );
  assert.match(
    stylesSource,
    /@media \(min-width:\s*700px\)[\s\S]*?\.new-session-dialog--panel\s*{[^}]*left:\s*24px[^}]*width:\s*auto\s*!important/s,
  );
});

test('half-width create panel uses the existing config menu below wide desktop', () => {
  assert.match(
    newSessionSource,
    /:force-config-menu="\s*\$q\.screen\.lt\.md \|\| \(panel && \$q\.screen\.width < overviewInlineConfigMinWidth\)\s*"/,
  );
  assert.match(newSessionSource, /const overviewInlineConfigMinWidth = 1536/);
  assert.match(
    readSource('../src/components/CodexPromptComposer.vue'),
    /:force-config-menu="forceConfigMenu"/,
  );
  assert.match(
    readSource('../src/components/PromptComposer.vue'),
    /!forceConfigMenu && \(!compact \|\| !\$q\.screen\.lt\.md\)/,
  );
});
