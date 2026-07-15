import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

const source = readFileSync(new URL('../src/services/diff.ts', import.meta.url), 'utf8');

function functionBody(name) {
  const start = source.indexOf(`export async function ${name}`);
  assert.notEqual(start, -1, `${name} body not found`);
  const next = source.indexOf('\nexport async function ', start + 1);
  return source.slice(start, next === -1 ? source.length : next);
}

test('getSessionDiffSummaries requests one normalized batch', () => {
  const body = functionBody('getSessionDiffSummaries');

  assert.match(body, /sessionDiffSummaries\(sessionIds: \$sessionIds\)/);
  assert.match(body, /sessionId/);
  assert.match(body, /state/);
  assert.match(body, /filesChanged/);
  assert.doesNotMatch(body, /Promise\.all/);
  assert.doesNotMatch(body, /sessionDiff\(input:/);
});

test('full diff service queries request only the needed diff shape', () => {
  const sessionSingle = functionBody('getSessionSingleDiff');
  const sessionAll = functionBody('getSessionAllDiff');
  const branchSingle = functionBody('getBranchSingleDiff');
  const branchAll = functionBody('getBranchAllDiff');

  assert.match(sessionSingle, /fileDiff\s*\{/);
  assert.doesNotMatch(sessionSingle, /allDiff\s*\{/);
  assert.match(sessionAll, /allDiff\s*\{/);
  assert.doesNotMatch(sessionAll, /fileDiff\s*\{/);
  assert.match(branchSingle, /fileDiff\s*\{/);
  assert.doesNotMatch(branchSingle, /allDiff\s*\{/);
  assert.match(branchAll, /allDiff\s*\{/);
  assert.doesNotMatch(branchAll, /fileDiff\s*\{/);
});
