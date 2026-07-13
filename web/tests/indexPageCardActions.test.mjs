import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { createOverviewCardGroups } from '../src/services/overviewCardGroups.js';

test('overview cards rely on the card click target instead of a duplicate detail button', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.equal(source.includes('aria-label="打开卡片"'), false);
  assert.equal(source.includes('打开卡片详情'), false);
});

test('overview card actions expose an explicit menu instead of a context-only menu', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.match(source, /aria-label="卡片操作"/);
  assert.doesNotMatch(source, /<q-menu\s+context-menu/);
  assert.match(source, /@keyup\.enter\.self=/);
  assert.match(source, /@keyup\.space\.self\.prevent=/);
});

test('overview requests latest and history ranges instead of dated buckets', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');
  const oldLatestRange = ['recent', '3d'].join('');
  const oldHistoryRange = ['history', '7d'].join('');

  assert.equal(source.includes(`range: '${oldLatestRange}'`), false);
  assert.equal(source.includes(`range: '${oldHistoryRange}'`), false);
  assert.equal(source.includes("range: 'latest'"), true);
  assert.equal(source.includes("range: 'history'"), true);
});

test('overview keeps closed cards out of latest and folds them into history', () => {
  const source = readFileSync(new URL('../src/pages/IndexPage.vue', import.meta.url), 'utf8');

  assert.equal(
    source.includes('createOverviewCardGroups(latestRows.value, historyRows.value)'),
    true,
  );
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
  const overviewSource = readFileSync(
    new URL('../src/pages/IndexPage.vue', import.meta.url),
    'utf8',
  );
  const sessionsSource = readFileSync(
    new URL('../src/pages/SessionsPage.vue', import.meta.url),
    'utf8',
  );

  assert.equal(overviewSource.includes("scope: 'closed'"), true);
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
  assert.match(overviewSource, /<q-tab name="output"[^>]*label="模型输出"/);
  assert.match(overviewSource, /<q-tab name="diff"[^>]*label="Diff"/);
  assert.match(overviewSource, /getSessionTranscriptPage\(card\.id, '', 10, 'assistant'\)/);
  assert.match(overviewSource, /getSessionAllDiff\(\{ sessionId: card\.id, mode: 'all'/);
  assert.match(overviewSource, /card\.pendingApproval/);
  assert.doesNotMatch(overviewSource, /workflow\.waiting_approval/);
  assert.match(overviewSource, /Promise\.allSettled/);
  assert.match(overviewSource, /approvalDiffTruncated/);
  assert.match(overviewSource, /approvalDiffs\.length === 0/);
  assert.match(overviewSource, /当前没有文件变更/);
  assert.match(overviewSource, /approvalOutputError/);
  assert.match(overviewSource, /<SessionEventMessage[^>]*:event="message"/);
  assert.match(overviewSource, /<WorkflowApprovalPanel/);
  assert.match(overviewSource, /aria-label="关闭人工审核"/);
  assert.match(
    overviewSource,
    /<q-dialog v-model="approvalDialog" :maximized="\$q\.screen\.lt\.sm">/,
  );
  assert.match(overviewSource, /submitWorkflowApproval\(/);
  assert.doesNotMatch(overviewSource, /approvalRejectPrompt|recentModelOutput/);
  assert.match(
    stylesSource,
    /\.forward-approval-dialog\s*{[^}]*width:\s*min\(960px, calc\(100vw - 32px\)\);/s,
  );
  assert.match(
    stylesSource,
    /\.forward-approval-dialog\s*{[^}]*max-width:\s*calc\(100vw - 32px\) !important;/s,
  );
  assert.match(sessionsSource, /pendingApproval\s*\{/);
  assert.match(sessionsSource, /workflowRunId/);
  assert.match(sessionsSource, /normalizePendingApproval/);
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
