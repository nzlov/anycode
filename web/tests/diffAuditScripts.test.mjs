import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

function readSource(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8');
}

function functionBody(source, name) {
  const start = source.indexOf(`async function ${name}`);
  assert.notEqual(start, -1, `${name} body not found`);
  const next = source.indexOf('\nasync function ', start + 1);
  return source.slice(start, next === -1 ? source.length : next);
}

const e2eSource = readSource('../../scripts/headless-e2e.mjs');
const clickAuditSource = readSource('../../scripts/headless-click-audit.mjs');

test('headless diff queries follow the unpaginated GraphQL contract', () => {
  for (const [source, name] of [
    [e2eSource, 'sessionDiff'],
    [clickAuditSource, 'sessionDiff'],
  ]) {
    const body = functionBody(source, name);
    assert.match(body, /files\s*{\s*path/s);
    assert.doesNotMatch(body, /files\s*{\s*items|pageInfo|pageSize|\bpage:/s);
  }
});

test('click audit verifies legacy Diff URL cleanup without pagination controls', () => {
  assert.match(clickAuditSource, /waitForRouteExcludes\('page='\)/);
  assert.match(clickAuditSource, /waitForRouteExcludes\('pageSize='\)/);
  assert.match(clickAuditSource, /clickAria\('展开全部文件'\)/);
  assert.match(clickAuditSource, /clickAria\('折叠全部文件'\)/);
  assert.doesNotMatch(clickAuditSource, /当前页全部文件/);
  assert.doesNotMatch(clickAuditSource, /function selectDiffPageSize|function clickDiffPage/);
});
