import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

const overview = readSource('../src/pages/IndexPage.vue');
const sessions = readSource('../src/pages/SessionsPage.vue');
const detail = readSource('../src/components/SessionDetailView.vue');
const questions = readSource('../src/components/QuestionsDialog.vue');
const horizontalConversation = readSource('../src/components/OverviewHorizontalConversation.vue');
const horizontalTerminal = readSource('../src/components/OverviewHorizontalTerminal.vue');
const forkButton = readSource('../src/components/SessionForkButton.vue');
const sessionService = readSource('../src/services/sessions.ts');

test('session queries carry the authoritative project git capability', () => {
  assert.match(sessionService, /export interface SessionCard[\s\S]*projectIsGit: boolean/);
  assert.match(sessionService, /const sessionCardFields = `[\s\S]*projectIsGit/);
  assert.match(sessionService, /const sessionDetailFields = `[\s\S]*projectIsGit/);
});

test('non-git overview and history cards hide branch and diff controls', () => {
  assert.equal((overview.match(/<div v-if="card\.projectIsGit">/g) ?? []).length, 2);
  assert.match(overview, /v-if="card\.projectIsGit && card\.filesChanged > 0"/);
  assert.match(horizontalConversation, /v-if="layout === 'desktop' && card\.projectIsGit"/);
  assert.match(horizontalTerminal, /v-if="card\.projectIsGit"[^>]*:title="card\.branch"/);
  assert.match(
    sessions,
    /<span v-if="props\.row\.projectIsGit">\{\{ props\.row\.branch \}\}<\/span>/,
  );
  assert.match(
    sessions,
    /<span v-if="props\.row\.projectIsGit">\{\{ props\.row\.filesChanged \}\}<\/span>/,
  );
  assert.match(sessions, /<q-btn\s+v-if="props\.row\.projectIsGit"[\s\S]*aria-label="查看 Diff"/);
  assert.match(sessions, /hasGitRows\.value \|\| \(name !== 'branch' && name !== 'diff'\)/);
});

test('non-git detail and review surfaces hide git-only navigation', () => {
  assert.match(detail, /<q-tab v-if="session\?\.projectIsGit" name="changes"/);
  assert.match(detail, /<q-item v-if="session\?\.projectIsGit">[\s\S]*<q-item-label caption>分支/);
  assert.match(detail, /<q-tab-panel\s+v-if="session\?\.projectIsGit"\s+name="changes"/);
  assert.match(overview, /<q-tab v-if="approvalProjectIsGit" name="diff"/);
  assert.match(questions, /<q-tab v-if="showDiff" name="diff"/);
  assert.match(forkButton, /<div v-if="projectIsGit"[\s\S]*Git 工作区/);
  assert.match(forkButton, /<div v-else[^>]*>新卡片继承当前 Codex 上下文。<\/div>/);
});
