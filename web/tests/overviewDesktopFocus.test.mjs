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

test('desktop overview keeps only latest cards and links history from the section heading', () => {
  assert.match(indexSource, /v-if="section\.id === 'latest' && showDesktopFocusLayout"/);
  assert.match(indexSource, /icon="history"/);
  assert.match(indexSource, /label="历史卡片"/);
  assert.match(indexSource, /:to="sessionsRoute"/);
  assert.match(indexSource, /const showDesktopFocusLayout = computed/);
  assert.match(
    indexSource,
    /showDesktopFocusLayout\.value\s*\? \[latestSection\]\s*:\s*\[latestSection, historySection\]/s,
  );
  assert.match(indexSource, /v-if="!hasAnyCards"/);
  assert.match(
    indexSource,
    /const hasAnyCards = computed\([\s\S]*latestCards\.value\.length > 0 \|\| historyCards\.value\.length > 0/,
  );
  assert.doesNotMatch(indexSource, /v-if="!hasVisibleCards"/);
});

test('desktop create panel overlays cards while reserving enough final scroll space', () => {
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
    /\.new-session-dialog--panel\s*{[^}]*position:\s*fixed[^}]*bottom:\s*24px[^}]*height:\s*var\(--overview-create-panel-height\)[^}]*z-index:/s,
  );
  assert.match(stylesSource, /@media \(min-width: 700px\) and \(max-width: 1023\.98px\)/);
  assert.match(stylesSource, /@media \(min-width: 1024px\)/);
});
