import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';

import { createOverviewCardGroups } from '../src/services/overviewCardGroups.js';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const layoutSource = readSource('../src/layouts/MainLayout.vue');
const indexSource = readSource('../src/pages/IndexPage.vue');
const viewModeSource = readSource('../src/composables/useOverviewViewMode.ts');
const horizontalSessionSource = readSource('../src/components/OverviewHorizontalSession.vue');
const desktopSessionSource = readSource('../src/components/OverviewHorizontalSessionDesktop.vue');
const mobileSessionSource = readSource('../src/components/OverviewHorizontalSessionMobile.vue');
const detailViewSource = readSource('../src/components/SessionDetailView.vue');
const detailPageSource = readSource('../src/pages/SessionDetailPage.vue');
const detailComposableSource = readSource('../src/composables/useSessionDetail.ts');
const stylesSource = readSource('../src/css/app.scss');
const schemaSource = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');
const newSessionSource = readSource('../src/components/NewSessionDialog.vue');

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

test('overview view mode persists across page changes and reloads', () => {
  const storage = new Map([['anycode.overview.view-mode.v1', 'horizontal']]);
  const originalWindow = globalThis.window;
  globalThis.window = {
    localStorage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
  };

  try {
    const compiled = ts.transpileModule(viewModeSource, {
      compilerOptions: {
        module: ts.ModuleKind.CommonJS,
        target: ts.ScriptTarget.ES2022,
      },
    }).outputText;
    const module = { exports: {} };
    new Function('require', 'module', 'exports', compiled)(
      createRequire(import.meta.url),
      module,
      module.exports,
    );

    const firstConsumer = module.exports.useOverviewViewMode();
    const secondConsumer = module.exports.useOverviewViewMode();
    assert.equal(firstConsumer.overviewViewMode.value, 'horizontal');
    firstConsumer.overviewViewMode.value = 'card';
    assert.equal(secondConsumer.overviewViewMode.value, 'card');
    assert.equal(storage.get('anycode.overview.view-mode.v1'), 'card');
  } finally {
    globalThis.window = originalWindow;
  }

  assert.match(layoutSource, /const \{ overviewViewMode \} = useOverviewViewMode\(\)/);
  assert.match(indexSource, /const \{ overviewViewMode \} = useOverviewViewMode\(\)/);
  assert.match(layoutSource, /view === 'horizontal'[\s\S]*overviewViewMode\.value = 'horizontal'/);
});

test('horizontal overview renders one independently resized component per visible session', () => {
  assert.match(indexSource, /<OverviewHorizontalSession[\s\S]*v-for="card in horizontalCards"/);
  assert.match(indexSource, /const minSessionColumnWidth = 320/);
  assert.match(indexSource, /@update:width="setSessionColumnWidth\(card\.id, \$event\)"/);
  assert.match(horizontalSessionSource, /role="separator"/);
  assert.match(horizontalSessionSource, /Math\.max\(props\.minWidth, Math\.round\(value\)\)/);
  assert.doesNotMatch(horizontalSessionSource, /maxWidth|aria-valuemax/);
  assert.match(stylesSource, /\.overview-horizontal-section\s*{[^}]*overflow-x:\s*auto/s);
  assert.match(stylesSource, /\.overview-horizontal-track\s*{[^}]*width:\s*max-content/s);
});

test('horizontal session columns keep identity order when live updates change recency', () => {
  const cards = [
    {
      id: 'older-session',
      status: 'running',
      createdAt: '2026-07-20T08:00:00Z',
      updatedTime: '2026-07-22T08:00:00Z',
    },
    {
      id: 'newer-session',
      status: 'running',
      createdAt: '2026-07-21T08:00:00Z',
      updatedTime: '2026-07-22T09:00:00Z',
    },
  ];
  const beforeAppend = createOverviewCardGroups(cards, []);
  const afterAppend = createOverviewCardGroups(
    cards.map((card) =>
      card.id === 'older-session' ? { ...card, updatedTime: '2026-07-22T11:00:00Z' } : card,
    ),
    [],
  );

  assert.deepEqual(
    beforeAppend.horizontalCards.map((card) => card.id),
    ['newer-session', 'older-session'],
  );
  assert.deepEqual(
    afterAppend.horizontalCards.map((card) => card.id),
    ['newer-session', 'older-session'],
  );
  assert.equal(beforeAppend.latestCards[0].id, 'newer-session');
  assert.equal(afterAppend.latestCards[0].id, 'older-session');
  assert.match(indexSource, /overviewCardGroups\.value\.horizontalCards/);
  assert.match(
    indexSource,
    /v-else-if="!isHorizontalView"[\s\S]*v-for="card in visibleLatestCards"/,
  );
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
  assert.match(indexSource, /<q-page-sticky v-if="!showDesktopFocusLayout"/);
  assert.match(indexSource, /:panel="showDesktopFocusLayout"/);
  assert.match(indexSource, /<q-btn[\s\S]*fab[\s\S]*aria-label="新建卡片"/);
});

test('successful desktop creation refreshes the new session in the mounted overview', () => {
  assert.match(
    newSessionSource,
    /const sessionId = await createSessionRequest\(input\);[\s\S]*?emit\('created', sessionId\)/,
  );
  assert.match(indexSource, /@created="refreshOverviewCard"/);
  assert.doesNotMatch(newSessionSource + indexSource, /anycode:session-created|handleSessionCreated/);
});
