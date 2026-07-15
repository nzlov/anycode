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
  assert.match(stylesSource, /\.overview-session-card--running\s*{[^}]*background:\s*#dcfce7;/s);
  assert.match(
    stylesSource,
    /\.overview-session-card--waiting_user\s*{[^}]*background:\s*#eeaa00;/s,
  );
  assert.doesNotMatch(stylesSource, /\.overview-session-card--(?:stopped|closed)\s*{/);
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
  assert.match(overviewSource, /card\.pendingApproval/);
  assert.doesNotMatch(overviewSource, /workflow\.waiting_approval/);
  assert.doesNotMatch(overviewSource, /approvalOutputError/);
  assert.match(overviewSource, /:phase="approvalPending\?\.phase \?\? null"/);
  assert.match(overviewSource, /:result="approvalPending\?\.result \?\? null"/);
  assert.match(overviewSource, /isPendingApprovalReviewable\(approvalPending\)/);
  assert.match(overviewSource, /!isPendingApprovalReviewable\(approvalPending\.value\)/);
  assert.match(overviewSource, /if \(approvalSubmitting\.value\) return;/);
  assert.match(overviewSource, /const requestGeneration = approvalContextGeneration/);
  assert.match(overviewSource, /approvalContext\.value\?\.workflowRunId === workflowRunId/);
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
  assert.match(sessionsSource, /workflowRunId/);
  assert.match(sessionsSource, /normalizePendingApproval/);
  assert.match(sessionsSource, /phase: approval\.phase/);
  assert.match(sessionsSource, /result: unknown/);
  assert.match(sessionsSource, /normalizeWorkflowNodeResult\(approval\.result\)/);
  assert.match(sessionsSource, /result,\s*\n\s*};/);
  assert.match(sessionsSource, /mutation SubmitWorkflowApproval/);

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
  assert.match(overviewSource, /getSessionDiffSummaries/);
  assert.match(
    overviewSource,
    /<q-dialog[\s\S]*?v-model="diffDialog"[\s\S]*?:maximized="\$q\.screen\.lt\.sm"/,
  );
  assert.match(overviewSource, /<DiffWorkspace[\s\S]*:target="diffDialogTarget"/);
  assert.match(overviewSource, /v-model="diffDialogWorkspaceState"/);
  assert.match(overviewSource, /aria-label="打开完整 Diff 页面"/);
  assert.match(
    overviewSource,
    /class="overview-card-secondary-actions"[\s\S]*overview-todo-btn[\s\S]*overview-diff-btn[\s\S]*class="overview-card-actions"/,
  );
  assert.doesNotMatch(
    overviewSource,
    /diffDialogDiffs|diffDialogLoading|diffDialogRequestGeneration/,
  );
  assert.match(workspaceSource, /class="diff-workspace"/);
  assert.doesNotMatch(workspaceSource, /<AppPagination|showPagination|modelValue\.page/);
  assert.match(workspaceSource, /<DiffViewer/);
  assert.match(workspaceSource, /getSessionAllDiff/);
  assert.match(workspaceSource, /getBranchAllDiff/);
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
  assert.doesNotMatch(detailSource, /<DiffViewer|getSessionDiffFiles|getSessionFileDiff/);
  assert.match(fileChangeSource, /<DiffViewer[^>]*:file-diffs="diffFileChanges"/);
  assert.doesNotMatch(fileChangeSource, /<DiffWorkspace|getSessionAllDiff|getSessionSingleDiff/);
  assert.doesNotMatch(answerDialogSource, /SessionDiffPreview/);
  assert.equal(overviewSource.includes('SessionDiffPreview'), false);
  assert.match(overviewSource, /class="overview-diff-dialog app-content-dialog"/);
  assert.match(stylesSource, /\.app-content-dialog\s*{[^}]*width:\s*90vw\s*!important/s);
  assert.match(stylesSource, /\.overview-diff-dialog\s*{[^}]*height:\s*90dvh/s);
  assert.match(stylesSource, /\.overview-diff-dialog__body\s*{[^}]*overflow:\s*hidden/s);
});
