import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { normalizeArtifactLogicalPath } from '../src/services/artifactLogicalPath.ts';

const panel = readFileSync(
  new URL('../src/components/SessionArtifactsPanel.vue', import.meta.url),
  'utf8',
);
const event = readFileSync(
  new URL('../src/components/SessionArtifactEvent.vue', import.meta.url),
  'utf8',
);
const eventMessage = readFileSync(
  new URL('../src/components/SessionEventMessage.vue', import.meta.url),
  'utf8',
);
const workflowResult = readFileSync(
  new URL('../src/components/WorkflowResultReview.vue', import.meta.url),
  'utf8',
);
const overviewPage = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');
const service = readFileSync(new URL('../src/services/sessionFiles.ts', import.meta.url), 'utf8');
const detailPage = readFileSync(
  new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
  'utf8',
);
const composer = readFileSync(
  new URL('../src/components/PromptComposer.vue', import.meta.url),
  'utf8',
);
const sessionsService = readFileSync(
  new URL('../src/services/sessions.ts', import.meta.url),
  'utf8',
);
const sessionDetailComposable = readFileSync(
  new URL('../src/composables/useSessionDetail.ts', import.meta.url),
  'utf8',
);

test('artifact surfaces consistently use the temporary file label', () => {
  const surfaces = [panel, event, eventMessage, workflowResult, overviewPage, detailPage].join(
    '\n',
  );
  assert.match(surfaces, /临时文件/);
  assert.doesNotMatch(surfaces, /产物/);
});

test('session artifacts use one unpaginated latest-version query and unified file actions', () => {
  assert.match(panel, /listSessionFiles\(input\)/);
  assert.match(panel, /input\.kind = kind\.value/);
  assert.match(panel, /input\.source = source\.value/);
  assert.doesNotMatch(panel, /mcp_artifact|published_artifact|reconciled_artifact/);
  assert.match(panel, /input\.sort = sort\.value/);
  assert.doesNotMatch(panel, /<AppPagination|pageSize|pageMax|result\.pageInfo/);
  assert.match(panel, /files\.value = result/);
  assert.match(service, /sessionFiles\(input: \$input\) \{ \$\{sessionFileFields\} \}/);
  assert.doesNotMatch(service, /sourceId/);
  assert.doesNotMatch(sessionsService, /sourceId/);
  assert.doesNotMatch(service, /SessionFilePage|pageInfo|pageSize/);
  assert.doesNotMatch(panel, /useSessionFileAsInput|copyAsInput/);
  assert.doesNotMatch(service, /useSessionFileAsInput/);
  assert.match(panel, /allowReference/);
  assert.match(panel, /emit\('referenceArtifact', file\)/);
  assert.match(panel, /deleteSessionFile/);
  assert.match(panel, /downloadSessionFile/);
});

test('authenticated previews revoke blob URLs and keep bounded text fully scrollable', () => {
  assert.match(service, /headers\.set\('authorization', `Bearer \$\{accessKey\}`\)/);
  assert.match(service, /URL\.revokeObjectURL\(url\)/);
  assert.match(panel, /URL\.revokeObjectURL\(previewURL\.value\)/);
  assert.match(event, /URL\.revokeObjectURL\(objectUrl\.value\)/);
  assert.match(panel, /file\.size > 1 << 20/);
  assert.match(panel, /\.artifact-text \{[\s\S]*?align-self: start;/);
  assert.match(event, /\.artifact-event-preview__body pre \{[\s\S]*?align-self: start;/);
  assert.match(panel, /selected\?\.previewKind === 'image'/);
  assert.match(panel, /selected\?\.previewKind === 'pdf'/);
  assert.match(panel, /selected\?\.previewKind === 'video'/);
  assert.match(panel, /selected\?\.previewKind === 'audio'/);
  assert.match(panel, /selected\?\.previewKind === 'text'/);
});

test('artifact panel enables one-item inline previews only for wide opted-in containers', () => {
  assert.match(panel, /inlinePreview\?: boolean/);
  assert.match(panel, /const inlinePreviewMinWidth = 1024/);
  assert.match(panel, /new ResizeObserver/);
  assert.match(panel, /width >= inlinePreviewMinWidth/);
  assert.match(panel, /v-if="inlinePreviewActive"/);
  assert.match(
    panel,
    /\.artifact-panel--inline-enabled \.artifact-layout\s*\{[^}]*grid-template-columns:\s*minmax\(320px,\s*36%\)\s+minmax\(0,\s*1fr\)/s,
  );
  assert.match(panel, /nextFiles\.find\(\(file\) => file\.id === selected\.value\?\.id\)/);
  assert.match(panel, /const first = nextFiles\[0\]/);
  assert.match(panel, /if \(first\) await selectPreview\(first\)/);
  assert.match(panel, /if \(!inlinePreviewActive\.value\) previewOpen\.value = true/);
  assert.match(panel, /panelResizeObserver\?\.disconnect\(\)/);
  assert.doesNotMatch(detailPage, /inline-preview/);
});

test('artifact controls and file actions reflow from the panel width', () => {
  assert.match(
    panel,
    /\.artifact-toolbar\s*\{[^}]*display:\s*grid[^}]*grid-template-columns:\s*minmax\(0,\s*1fr\)\s+repeat\(3,\s*minmax\(112px,\s*160px\)\)\s+40px/s,
  );
  assert.match(
    panel,
    /@container \(max-width:\s*639px\)[\s\S]*?\.artifact-toolbar\s*\{[^}]*grid-template-columns:\s*repeat\(2,\s*minmax\(0,\s*1fr\)\)\s+40px/s,
  );
  assert.match(
    panel,
    /@container \(max-width:\s*639px\)[\s\S]*?\.artifact-list-item\s*\{[^}]*grid-template-columns:\s*40px\s+minmax\(0,\s*1fr\)/s,
  );
  assert.doesNotMatch(panel, /@media \(max-width:\s*599px\)/);
});

test('artifact requests ignore stale responses and follow live artifact events', () => {
  assert.match(service, /signal\?: AbortSignal/);
  assert.match(service, /fetch\(url, \{ headers, signal: signal \?\? null \}\)/);
  assert.match(panel, /const request = \+\+loadRequest/);
  assert.match(panel, /request !== loadRequest/);
  assert.match(panel, /previewController\?\.abort\(\)/);
  assert.match(event, /previewController\?\.abort\(\)/);
  assert.match(detailPage, /:refresh-key="artifactRefreshKey"/);
  assert.match(
    sessionDetailComposable,
    /typeof update\.artifactCount === 'number'[\s\S]*artifactUpdateVersion\.value \+= 1/,
  );
  assert.doesNotMatch(
    sessionDetailComposable,
    /refreshAfterReconnect|lateRegistered|registration\.ready/,
  );
  assert.match(
    detailPage,
    /artifactRefreshKey = computed\(\(\) => String\(artifactUpdateVersion\.value\)\)/,
  );
  assert.doesNotMatch(detailPage, /entry\.content\.rawType\.startsWith\('artifact\.'\)/);
});

test('artifact references normalize only safe relative logical paths', () => {
  assert.equal(normalizeArtifactLogicalPath(' reports\\result.txt '), 'reports/result.txt');
  assert.equal(normalizeArtifactLogicalPath('reports//result.txt'), 'reports/result.txt');
  assert.equal(normalizeArtifactLogicalPath('/absolute.txt'), null);
  assert.equal(normalizeArtifactLogicalPath('../escape.txt'), null);
  assert.equal(normalizeArtifactLogicalPath('a/../escape.txt'), null);
});

test('artifact panel supports controlled focus without replacing file actions', () => {
  assert.match(panel, /focusRequest/);
  assert.match(panel, /props\.focusRequest\?\.token/);
  assert.match(panel, /void applyFocus\(request\)/);
  assert.match(panel, /\{ immediate: true \}/);
  assert.match(panel, /emit\('artifactDeleted', file\)/);
  assert.match(panel, /emit\('artifactsRefreshed'\)/);
  assert.match(panel, /@click="refresh"/);
  assert.match(panel, /openPreview\(request\.file\)/);
});

test('detail prompt composer submits artifact ids without uploading artifacts', () => {
  assert.match(detailPage, /v-model:artifacts="appendArtifacts"/);
  assert.match(detailPage, /allow-reference/);
  assert.match(detailPage, /selectedArtifacts\.map\(\(artifact\) => artifact\.id\)/);
  assert.match(composer, /v-for="artifact in artifacts"/);
  assert.match(composer, /attachmentCount/);
  assert.match(sessionsService, /artifactIds\?: string\[\]/);
  assert.match(sessionsService, /input\.artifactIds = artifactIds/);
  assert.doesNotMatch(detailPage, /stageAttachment\(artifact/);
});
