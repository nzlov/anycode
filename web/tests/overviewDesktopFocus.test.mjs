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

test('desktop overview replaces the create FAB with the persistent create panel', () => {
  assert.match(
    layoutSource,
    /v-if="\$route\.name === 'overview' && \$q\.screen\.width < overviewDesktopMinWidth"/,
  );
  assert.match(layoutSource, /:panel="showOverviewCreatePanel"/);
  assert.match(layoutSource, /const overviewDesktopMinWidth = 700/);
  assert.match(layoutSource, /const showOverviewCreatePanel = computed/);

  assert.match(newSessionSource, /panel\?: boolean/);
  assert.match(newSessionSource, /:model-value="dialogVisible"/);
  assert.match(newSessionSource, /:seamless="panel"/);
  assert.match(newSessionSource, /:no-focus="panel"/);
  assert.match(newSessionSource, /new-session-dialog--panel/);
  assert.match(newSessionSource, /const dialogVisible = computed/);
});

test('overview keeps only latest cards and links history from the filter toolbar', () => {
  assert.match(indexSource, /class="overview-filter-toolbar"/);
  assert.match(indexSource, /icon="history"/);
  assert.match(indexSource, /aria-label="历史卡片"/);
  assert.doesNotMatch(indexSource, /\slabel="历史卡片"/);
  assert.match(indexSource, /:to="sessionsRoute"/);
  assert.match(indexSource, /const showDesktopFocusLayout = computed/);
  assert.match(indexSource, /v-for="card in visibleLatestCards"/);
  assert.doesNotMatch(indexSource, /historySection|historyCards|hasMoreHistory/);
});

test('desktop create panel uses one project row within its reserved maximum height', () => {
  assert.match(
    stylesSource,
    /:root\s*{[^}]*--overview-create-panel-height:\s*min\(420px, calc\(100dvh - 104px\)\)/s,
  );
  assert.match(
    stylesSource,
    /\.page-shell\.workbench-page--desktop-focus\s*{[^}]*padding-bottom:\s*calc\(var\(--overview-create-panel-height\) \+ 48px\)/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel\s*{[^}]*position:\s*fixed[^}]*bottom:\s*24px[^}]*height:\s*auto[^}]*max-height:\s*var\(--overview-create-panel-height\)\s*!important[^}]*z-index:/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-dialog--panel \.new-session-body\s*{[^}]*flex:\s*1 1 auto[^}]*overflow-y:\s*auto/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-panel-host \.new-session-dialog--panel\s*{[^}]*overflow:\s*visible/s,
  );
  assert.match(
    stylesSource,
    /\.new-session-grid\s*{[^}]*grid-template-columns:\s*minmax\(0, 1fr\)[^}]*grid-auto-flow:\s*column/s,
  );
  assert.match(
    newSessionSource,
    /label="项目"[\s\S]*class="branch-picker"[\s\S]*label="优先级"[\s\S]*class="new-session-mode"/,
  );
  assert.match(
    stylesSource,
    /@media \(min-width: 700px\)[\s\S]*?\.new-session-dialog--panel\s*{[^}]*left:\s*24px/s,
  );
});
