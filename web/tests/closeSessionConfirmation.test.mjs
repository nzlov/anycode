import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

test('closing a dirty worktree requires an explicit user confirmation before retrying', () => {
  const schema = readSource('../../internal/interfaces/graphql/graph/schema.graphqls');
  const sessions = readSource('../src/services/sessions.ts');
  const confirmation = readSource('../src/composables/useConfirmedSessionClose.ts');

  assert.match(schema, /input CloseSessionInput \{[\s\S]*confirmWorktreeClose: Boolean/);
  assert.match(sessions, /confirmWorktreeClose = false/);
  assert.match(sessions, /confirmWorktreeClose \}/);
  assert.match(confirmation, /worktree_close_confirmation_required/);
  assert.match(confirmation, /工作树有未提交文件/);
  assert.match(confirmation, /所有未提交的修改都将丢失/);
  assert.match(confirmation, /await closeSession\(sessionId, reason, true\)/);
});

test('every manual close entry point uses the confirmed close flow', () => {
  for (const relativePath of [
    '../src/pages/IndexPage.vue',
    '../src/composables/useSessionDetail.ts',
    '../src/components/TerminalSessionView.vue',
  ]) {
    const source = readSource(relativePath);
    assert.match(source, /closeSessionWithConfirmation/);
  }
});
