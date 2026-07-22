import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layoutSource = readSource('../src/layouts/MainLayout.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const horizontalSessionSource = readSource('../src/components/OverviewHorizontalSession.vue');
const desktopSessionSource = readSource('../src/components/OverviewHorizontalSessionDesktop.vue');
const mobileSessionSource = readSource('../src/components/OverviewHorizontalSessionMobile.vue');
const detailViewSource = readSource('../src/components/SessionDetailView.vue');
const detailPageSource = readSource('../src/pages/SessionDetailPage.vue');
const detailComposableSource = readSource('../src/composables/useSessionDetail.ts');
const stylesSource = readSource('../src/css/app.scss');
const schemaSource = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');

test('desktop header switches between card and horizontal overview modes beside history', () => {
  assert.match(
    layoutSource,
    /icon="history"[\s\S]*?:icon="isOverviewHorizontalView \? 'grid_view' : 'view_column'"/,
  );
  assert.match(layoutSource, /@click="toggleOverviewView"/);
  assert.match(layoutSource, /query\.view = 'horizontal'/);
  assert.match(layoutSource, /delete query\.view/);
  assert.match(layoutSource, /\$q\.screen\.width >= overviewDesktopMinWidth/);
});

test('horizontal overview renders one independently resized component per visible session', () => {
  assert.match(indexSource, /<OverviewHorizontalSession[\s\S]*v-for="card in visibleLatestCards"/);
  assert.match(indexSource, /const minSessionColumnWidth = 320/);
  assert.match(indexSource, /@update:width="setSessionColumnWidth\(card\.id, \$event\)"/);
  assert.match(horizontalSessionSource, /role="separator"/);
  assert.match(horizontalSessionSource, /Math\.max\(props\.minWidth, Math\.round\(value\)\)/);
  assert.doesNotMatch(horizontalSessionSource, /maxWidth|aria-valuemax/);
  assert.match(stylesSource, /\.overview-horizontal-section\s*{[^}]*overflow-x:\s*auto/s);
  assert.match(stylesSource, /\.overview-horizontal-track\s*{[^}]*width:\s*max-content/s);
});

test('single horizontal session selects dedicated mobile and desktop components by column width', () => {
  assert.match(horizontalSessionSource, /const desktopSessionMinWidth = 1024/);
  assert.match(
    horizontalSessionSource,
    /props\.width >= desktopSessionMinWidth \? 'desktop' : 'mobile'/,
  );
  assert.match(horizontalSessionSource, /<OverviewHorizontalSessionMobile[\s\S]*v-if=/);
  assert.match(horizontalSessionSource, /<OverviewHorizontalSessionDesktop[\s\S]*v-else/);
  assert.match(mobileSessionSource, /<SessionDetailView[\s\S]*layout="mobile"/);
  assert.match(desktopSessionSource, /<SessionDetailView[\s\S]*layout="desktop"/);
});

test('horizontal sessions render the complete reusable detail surface', () => {
  assert.match(detailPageSource, /<SessionDetailView[\s\S]*layout="responsive"[\s\S]*page/);
  assert.match(detailViewSource, /<SessionEventMessage/);
  assert.match(detailViewSource, /<CodexPromptComposer/);
  assert.match(detailViewSource, /<q-tab name="info"[^>]*label="会话信息"/);
  assert.match(detailViewSource, /<q-tab name="changes"[^>]*label="当前变更"/);
  assert.match(detailViewSource, /<q-tab name="artifacts"[^>]*label="临时文件"/);
  assert.match(detailViewSource, /<DiffWorkspace/);
  assert.match(detailViewSource, /<SessionArtifactsPanel/);
  assert.match(detailViewSource, /class="append-history"/);
});

test('horizontal mode keeps one existing detail subscription per session', () => {
  assert.match(mobileSessionSource, /:session-id="card\.id"/);
  assert.match(desktopSessionSource, /:session-id="card\.id"/);
  assert.match(detailViewSource, /useSessionDetail\(sessionId\)/);
  assert.match(detailComposableSource, /subscribeSessionEvents\(sessionId/);
  assert.match(schemaSource, /sessionEvents\(sessionId: ID!\): TranscriptEvent!/);
  assert.doesNotMatch(schemaSource, /allSessionEvents/);
  assert.doesNotMatch(indexSource, /getSessionTranscriptPage|horizontalEvents/);
  assert.doesNotMatch(detailComposableSource, /subscribeAllSessionEvents|allSessionEvents/);
});

test('horizontal mode uses the mobile-style create FAB and disables the persistent prompt panel', () => {
  assert.match(
    layoutSource,
    /\(\$q\.screen\.width < overviewDesktopMinWidth \|\| isOverviewHorizontalView\)/,
  );
  assert.match(
    layoutSource,
    /showOverviewCreatePanel = computed\([\s\S]*?!isOverviewHorizontalView\.value/,
  );
  assert.match(layoutSource, /<q-btn[\s\S]*fab[\s\S]*aria-label="新建卡片"/);
});
