import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { createOverviewCardGroups } from '../src/services/overviewCardGroups.js';

test('overview cards rely on the card click target instead of a duplicate detail button', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.equal(source.includes('aria-label="打开卡片"'), false);
  assert.equal(source.includes('打开卡片详情'), false);
});

test('overview card actions use a context menu without a visible trigger', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.doesNotMatch(source, /aria-label="卡片操作"/);
  assert.doesNotMatch(source, /icon="more_vert"/);
  assert.match(source, /<q-menu\s+context-menu/);
  assert.match(
    source,
    /class="overview-todo-btn app-command-btn"[\s\S]*?@contextmenu\.stop[\s\S]*?@touchstart\.stop/,
  );
  assert.match(
    source,
    /class="overview-card-actions"[\s\S]*?@contextmenu\.stop[\s\S]*?@touchstart\.stop/,
  );
  assert.match(source, /@click="openSessionCard\(card\.id\)"/);
  assert.match(source, /@touchend="releaseCardContextMenuTouch\(card\.id\)"/);
  assert.match(source, /@before-show="handleCardContextMenuBeforeShow\(card\.id, \$event\)"/);
  assert.match(source, /在新标签页中打开/);
  assert.match(source, /target="_blank"/);
  assert.match(source, /rel="noopener noreferrer"/);
  assert.match(source, /GLUE: suppress Quasar's synthetic post-long-press click/);
  assert.match(source, /@keyup\.enter\.self=/);
  assert.match(source, /@keyup\.space\.self\.prevent=/);
});

test('overview TODO menu supports hover without removing click, touch, or keyboard access', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.match(source, /const activeTodoMenuId = ref\(''\);/);
  assert.match(source, /const todoMenuHideDelay = 120;/);
  assert.match(
    source,
    /class="overview-todo-btn app-command-btn"[\s\S]*?@pointerenter="openTodoMenu\(card\.id, \$event\)"[\s\S]*?@pointerleave="scheduleTodoMenuClose\(card\.id, \$event\)"[\s\S]*?@click\.stop="toggleTodoMenu\(card\.id\)"[\s\S]*?@touchstart\.stop[\s\S]*?@keyup\.enter\.stop[\s\S]*?@keyup\.space\.stop/,
  );
  assert.match(
    source,
    /<q-menu[\s\S]*?no-parent-event[\s\S]*?no-focus[\s\S]*?:model-value="activeTodoMenuId === card\.id"[\s\S]*?@update:model-value="syncTodoMenuModel\(card\.id, \$event\)"[\s\S]*?@pointerenter="openTodoMenu\(card\.id, \$event\)"[\s\S]*?@pointerleave="scheduleTodoMenuClose\(card\.id, \$event\)"/,
  );
  assert.match(source, /if \(event\.pointerType !== 'mouse'\) return;/);
  assert.match(source, /onUnmounted\(\(\) => \{[\s\S]*?clearTodoMenuHideTimer\(\)/);
  assert.doesNotMatch(source, /<q-tooltip>TODO List<\/q-tooltip>/);
});

test('overview TODO headless scenario covers pointer, keyboard, and narrow-screen access', () => {
  const source = readFileSync(new URL('../../scripts/headless-e2e.mjs', import.meta.url), 'utf8');

  assert.match(source, /await assertOverviewTodoMenuInteractions\(\);/);
  assert.match(source, /Input\.dispatchMouseEvent[\s\S]*overview TODO opens on hover/);
  assert.match(source, /overview TODO closes while pointer is over the menu/);
  assert.match(source, /overview TODO closes after pointer leaves/);
  assert.match(source, /overview TODO opens on click/);
  assert.match(source, /key: 'Enter'[\s\S]*overview TODO opens with Enter/);
  assert.match(source, /key: ' ', code: 'Space'[\s\S]*overview TODO opens with Space/);
  assert.match(source, /key: 'Escape'[\s\S]*overview TODO closes with Escape/);
  assert.match(source, /Input\.dispatchTouchEvent[\s\S]*overview TODO opens on touch/);
});

test('overview requests only the latest range instead of dated buckets', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');
  const oldLatestRange = ['recent', '3d'].join('');
  const oldHistoryRange = ['history', '7d'].join('');

  assert.equal(source.includes(`range: '${oldLatestRange}'`), false);
  assert.equal(source.includes(`range: '${oldHistoryRange}'`), false);
  assert.equal(source.includes("range: 'latest'"), true);
  assert.equal(source.includes("range: 'history'"), false);
});

test('overview keeps closed cards out of latest without loading history cards', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.equal(source.includes('createOverviewCardGroups(latestRows.value, [])'), true);
});

test('overview groups latest and history cards by closed state and last operation', () => {
  const latestRows = [
    { id: 'open-older', status: 'stopped', updatedTime: '2026-07-08T08:00:00Z' },
    { id: 'open-newer', status: 'running', updatedTime: '2026-07-08T10:00:00Z' },
    { id: 'closed-newer', status: 'closed', updatedTime: '2026-07-08T12:00:00Z' },
  ];
  const historyRows = [
    { id: 'closed-older', status: 'closed', updatedTime: '2026-07-08T09:00:00Z' },
    { id: 'closed-newer', status: 'closed', updatedTime: '2026-07-08T12:00:00Z' },
    { id: 'closed-middle', status: 'closed', updatedTime: '2026-07-08T11:00:00Z' },
  ];

  const groups = createOverviewCardGroups(latestRows, historyRows);

  assert.deepEqual(
    groups.latestCards.map((card) => card.id),
    ['open-newer', 'open-older'],
  );
  assert.deepEqual(
    groups.historyCards.map((card) => card.id),
    ['closed-newer', 'closed-middle', 'closed-older'],
  );
});

test('history more link opens the sessions table with closed scope', () => {
  const layoutSource = readFileSync(
    new URL('../src/layouts/MainLayout.vue', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/pages/SessionsPage.vue', import.meta.url),
    'utf8',
  );

  assert.equal(layoutSource.includes("scope: 'closed'"), true);
  assert.equal(sessionsSource.includes("{ label: '已关闭', value: 'closed' }"), true);
  assert.match(sessionsSource, /route\.query\.scope/);
});

test('overview card backgrounds highlight running and waiting answer states only', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const stylesSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(overviewSource, /overviewCardClass\(card\)/);
  assert.match(
    stylesSource,
    /\.overview-session-card--running\s*{[^}]*background:\s*var\(--ac-status-success-bg\);/s,
  );
  assert.match(
    stylesSource,
    /\.overview-session-card--waiting_user\s*{[^}]*background:\s*var\(--ac-status-warning-bg\);/s,
  );
  assert.doesNotMatch(stylesSource, /\.overview-session-card--(?:stopped|closed)\s*{/);
});

test('overview waiting answer card uses one status badge', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const chips = overviewSource.slice(
    overviewSource.indexOf('<div class="overview-card-chips">'),
    overviewSource.indexOf('<div class="overview-card-title">'),
  );

  assert.equal((chips.match(/statusLabel\(card\.status\)/g) ?? []).length, 1);
  assert.doesNotMatch(chips, /v-if="card\.status === 'waiting_user'"/);
});

test('overview waiting approval dialog shows model output and diff before submit', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const approvalPanelSource = readFileSync(
    new URL('../src/components/WorkflowApprovalPanel.vue', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const stylesSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(overviewSource, /card\.status === 'waiting_approval'/);
  assert.match(overviewSource, /openApprovalDialog\(card\)/);
  assert.match(overviewSource, /<q-tab name="output"[^>]*label="审核结果"/);
  assert.match(overviewSource, /<q-tab name="diff"[^>]*label="Diff"/);
  assert.match(overviewSource, /<q-tab name="artifacts"[^>]*label="产物"/);
  assert.match(overviewSource, /<SessionArtifactsPanel/);
  assert.match(overviewSource, /resolveSessionArtifacts/);
  assert.match(overviewSource, /approvalArtifactFocus/);
  assert.match(overviewSource, /@open-artifact="openApprovalArtifact"/);
  assert.match(overviewSource, /@artifact-deleted="refreshApprovalArtifactReferences"/);
  assert.match(overviewSource, /@artifacts-refreshed="refreshApprovalArtifactReferences"/);
  assert.match(overviewSource, /approvalContextGeneration === requestGeneration/);
  assert.doesNotMatch(overviewSource, /getSessionTranscriptPage/);
  assert.doesNotMatch(overviewSource, /getSessionAllDiff/);
  assert.match(overviewSource, /const detail = await getSession\(card\.id\)/);
  assert.match(overviewSource, /approvalPending\.value = detail\.pendingApproval \?\? null/);
  assert.doesNotMatch(overviewSource, /card\.pendingApproval/);
  assert.doesNotMatch(overviewSource, /workflow\.waiting_approval/);
  assert.doesNotMatch(overviewSource, /approvalOutputError/);
  assert.match(overviewSource, /:phase="approvalPending\?\.phase \?\? null"/);
  assert.match(overviewSource, /:result="approvalPending\?\.result \?\? null"/);
  assert.match(overviewSource, /isPendingApprovalReviewable\(approvalPending\)/);
  assert.match(overviewSource, /!isPendingApprovalReviewable\(approvalPending\.value\)/);
  assert.match(overviewSource, /if \(approvalSubmitting\.value\) return;/);
  assert.match(overviewSource, /const requestGeneration = approvalContextGeneration/);
  assert.match(overviewSource, /approvalContext\.value\?\.sessionId === workflowSessionId/);
  assert.match(overviewSource, /approvalContext\.value\?\.nodeId === nodeId/);
  assert.match(overviewSource, /<DiffWorkspace[\s\S]*v-model="approvalDiffWorkspaceState"/);
  assert.match(overviewSource, /:target="approvalDiffTarget"/);
  assert.doesNotMatch(
    overviewSource,
    /approvalDiffs|approvalDiffAvailable|approvalDiffTotal|approvalDiffError/,
  );
  assert.match(overviewSource, /<WorkflowApprovalPanel/);
  assert.match(overviewSource, /aria-label="关闭人工审核"/);
  assert.match(
    overviewSource,
    /<q-dialog[\s\S]*?v-model="approvalDialog"[\s\S]*?:maximized="\$q\.screen\.lt\.sm"[\s\S]*?@hide="handleApprovalDialogClosed"/,
  );
  assert.match(overviewSource, /submitWorkflowApproval\(/);
  assert.doesNotMatch(overviewSource, /approvalRejectPrompt|recentModelOutput/);
  assert.match(overviewSource, /class="forward-approval-dialog app-content-dialog"/);
  assert.match(
    stylesSource,
    /\.app-content-dialog\s*{[^}]*width:\s*90vw\s*!important[^}]*max-width:\s*90vw\s*!important/s,
  );
  assert.match(sessionsSource, /pendingApproval\s*\{/);
  assert.match(sessionsSource, /sessionId/);
  assert.match(sessionsSource, /normalizePendingApproval/);
  assert.match(sessionsSource, /phase: approval\.phase/);
  assert.match(sessionsSource, /result: unknown/);
  assert.match(sessionsSource, /normalizeWorkflowNodeResult\(approval\.result\)/);
  assert.match(sessionsSource, /result,\s*\n\s*};/);
  assert.match(sessionsSource, /mutation SubmitWorkflowApproval/);
  assert.match(sessionsSource, /submitWorkflowApproval\(input: \$input\)\s*{\s*sessionId/);
  assert.doesNotMatch(sessionsSource, /submitWorkflowApproval\(input: \$input\)\s*{\s*id\b/);
  assert.doesNotMatch(sessionsSource, /submitWorkflowApproval:\s*{\s*id:\s*string/);

  assert.match(approvalPanelSource, /mode === 'reject'/);
  assert.match(approvalPanelSource, /label="返回"/);
  assert.match(approvalPanelSource, /label="确认拒绝"/);
  assert.match(approvalPanelSource, /rejectPrompt\.trim\(\) === ''/);
  assert.doesNotMatch(
    approvalPanelSource,
    /function returnToDecision[\s\S]*rejectPrompt\.value\s*=\s*''/,
  );
});

test('overview waiting answer dialog shows questions and diff before submit', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const answerDialogSource = readFileSync(
    new URL('../src/components/AnswerUserDialog.vue', import.meta.url),
    'utf8',
  );

  assert.match(answerDialogSource, /<q-tab name="questions"[^>]*label="问题"/);
  assert.match(answerDialogSource, /<q-tab name="diff"[^>]*label="Diff"/);
  assert.match(answerDialogSource, /<AnswerUserPanel/);
  assert.match(answerDialogSource, /<DiffWorkspace[\s\S]*v-model="diffWorkspaceState"/);
  assert.match(answerDialogSource, /:target="diffTarget"/);
  assert.match(answerDialogSource, /:to="fullDiffRoute"/);
  assert.match(overviewSource, /<AnswerUserDialog[\s\S]*:diff-target="answerDiffTarget"/);
  assert.match(overviewSource, /getPendingQuestionBatches\(sessionId\)/);
  assert.match(overviewSource, /const requestGeneration = \+\+questionRequestGeneration/);
  assert.match(overviewSource, /activeQuestionSessionId\.value === sessionId/);
  assert.doesNotMatch(overviewSource, /answerDiffLoading|answerDiffs|answerDiffError/);
  assert.match(
    overviewSource,
    /query: \{ sessionId: activeQuestionSessionId\.value, mode: 'all' \}/,
  );
});

test('all current diff surfaces reuse one workspace without triggering card navigation', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const answerDialogSource = readFileSync(
    new URL('../src/components/AnswerUserDialog.vue', import.meta.url),
    'utf8',
  );
  const workspaceSource = readFileSync(
    new URL('../src/components/DiffWorkspace.vue', import.meta.url),
    'utf8',
  );
  const diffPageSource = readFileSync(
    new URL('../src/pages/DiffPage.vue', import.meta.url),
    'utf8',
  );
  const detailSource = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );
  const fileChangeSource = readFileSync(
    new URL('../src/components/SessionFileChangeEvent.vue', import.meta.url),
    'utf8',
  );
  const stylesSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(overviewSource, /v-if="card\.filesChanged > 0"/);
  assert.match(overviewSource, /icon="difference"/);
  assert.match(overviewSource, /:label="String\(card\.filesChanged\)"/);
  assert.match(overviewSource, /:aria-label="`查看 \$\{card\.filesChanged\} 个变更文件`"/);
  assert.match(overviewSource, /@click\.stop="openDiffDialog\(card\)"/);
  assert.match(overviewSource, /@keyup\.enter\.stop/);
  assert.match(overviewSource, /@keyup\.space\.stop/);
  assert.doesNotMatch(overviewSource, /loadSummaries|syncPolling|visibilitychange/);
  assert.match(
    overviewSource,
    /<q-dialog[\s\S]*?v-model="diffDialog"[\s\S]*?:maximized="\$q\.screen\.lt\.sm"/,
  );
  assert.match(overviewSource, /<DiffWorkspace[\s\S]*:target="diffDialogTarget"/);
  assert.match(overviewSource, /v-model="diffDialogWorkspaceState"/);
  assert.match(overviewSource, /aria-label="打开完整 Diff 页面"/);
  assert.match(
    overviewSource,
    /class="overview-card-secondary-actions"[\s\S]*overview-todo-btn[\s\S]*overview-diff-btn[\s\S]*overview-artifact-btn[\s\S]*class="overview-card-actions"/,
  );
  assert.doesNotMatch(
    overviewSource,
    /diffDialogDiffs|diffDialogLoading|diffDialogRequestGeneration/,
  );
  assert.match(workspaceSource, /class="diff-workspace"/);
  assert.doesNotMatch(workspaceSource, /<AppPagination|showPagination|modelValue\.page/);
  assert.match(workspaceSource, /<DiffViewer/);
  assert.match(workspaceSource, /getSessionAllDiff/);
  assert.match(workspaceSource, /getSessionDiffFiles/);
  assert.match(workspaceSource, /getSessionSingleDiff/);
  assert.match(workspaceSource, /getBranchAllDiff/);
  assert.match(
    workspaceSource,
    /metadataFirst\.value[\s\S]*getSessionDiffFiles[\s\S]*initialDiffCollapseState\(targetKey\.value\)/,
  );
  assert.match(
    workspaceSource,
    /toggleFileCollapsed[\s\S]*await loadFileDiff\(filePath\)[\s\S]*toggleDiffFileCollapsed/,
  );
  assert.match(workspaceSource, /expandAllFiles[\s\S]*void loadAllDiff\(\)/);
  assert.match(workspaceSource, /aria-label="展开全部文件"/);
  assert.match(workspaceSource, /aria-label="折叠全部文件"/);
  assert.match(workspaceSource, /GLUE: branch Diff paths encode their source session/);
  assert.doesNotMatch(workspaceSource, /sessionPrefixTargetKey/);
  assert.match(diffPageSource, /<DiffWorkspace/);
  assert.doesNotMatch(diffPageSource, /class="diff-layout"/);
  assert.doesNotMatch(diffPageSource, /<AppPagination|<DiffViewer/);
  assert.doesNotMatch(diffPageSource, /getSessionAllDiff|getBranchAllDiff/);
  assert.doesNotMatch(stylesSource, /\.diff-layout/);
  assert.match(answerDialogSource, /<DiffWorkspace/);
  assert.match(detailSource, /<DiffWorkspace[\s\S]*:target="detailDiffTarget"/);
  assert.match(detailSource, /:show-file-navigation="false"/);
  assert.match(detailSource, /lazy-file-details/);
  assert.doesNotMatch(detailSource, /<DiffViewer|getSessionDiffFiles|getSessionFileDiff/);
  assert.match(fileChangeSource, /<DiffViewer[^>]*:file-diffs="diffFileChanges"/);
  assert.doesNotMatch(fileChangeSource, /<DiffWorkspace|getSessionAllDiff|getSessionSingleDiff/);
  assert.doesNotMatch(answerDialogSource, /SessionDiffPreview/);
  assert.equal(overviewSource.includes('SessionDiffPreview'), false);
  assert.match(overviewSource, /class="overview-diff-dialog app-content-dialog"/);
  assert.match(stylesSource, /\.app-content-dialog\s*{[^}]*width:\s*90vw\s*!important/s);
  assert.match(
    stylesSource,
    /\.overview-diff-dialog,\s*\.overview-artifact-dialog\s*{[^}]*height:\s*90dvh/s,
  );
  assert.match(stylesSource, /\.overview-diff-dialog__body\s*{[^}]*overflow:\s*hidden/s);
});

test('overview cards open the full artifact panel from a subscribed count', () => {
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const sessionSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const stylesSource = readFileSync(new URL('../src/css/app.scss', import.meta.url), 'utf8');

  assert.match(sessionSource, /artifactCount/);
  assert.match(sessionSource, /artifactCount:\s*Math\.max\(0, session\.artifactCount\)/);
  assert.match(sessionSource, /filesChanged:\s*Math\.max\(0, session\.filesChanged\)/);
  assert.match(overviewSource, /v-if="card\.artifactCount > 0"/);
  assert.match(overviewSource, /class="overview-artifact-btn app-command-btn"/);
  assert.match(overviewSource, /:label="String\(card\.artifactCount\)"/);
  assert.match(overviewSource, /@click\.stop="openArtifactDialog\(card\)"/);
  assert.match(
    overviewSource,
    /<SessionArtifactsPanel[\s\S]*:session-id="artifactDialogSessionId"/,
  );
  assert.equal(overviewSource.match(/\binline-preview\b/g)?.length, 1);
  assert.match(overviewSource, /:session-id="artifactDialogSessionId"[\s\S]*?inline-preview/);
  assert.match(
    overviewSource,
    /<q-dialog[\s\S]*v-model="artifactDialog"[\s\S]*:maximized="\$q\.screen\.lt\.sm"/,
  );
  assert.match(stylesSource, /\.overview-artifact-dialog__body\s*{[^}]*overflow:\s*auto/s);
});
