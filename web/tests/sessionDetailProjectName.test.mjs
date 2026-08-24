import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

test('session detail displays the project name returned by its query', () => {
  const source = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');
  const detailFields = /const sessionDetailFields = `(?<fields>[\s\S]*?)`/.exec(source);
  const normalizer = /function normalizeSessionDetail[\s\S]*?\n}/.exec(source);

  assert.match(detailFields?.groups?.fields ?? '', /\bprojectName\b/);
  assert.match(normalizer?.[0] ?? '', /projectName: session\.projectName/);
  assert.doesNotMatch(normalizer?.[0] ?? '', /projectName: session\.projectId/);
});

test('forked session detail displays its source card id as an information link', () => {
  const serviceSource = readFileSync(
    new URL('../src/services/sessions.ts', import.meta.url),
    'utf8',
  );
  const detailSource = readFileSync(
    new URL('../src/components/SessionDetailView.vue', import.meta.url),
    'utf8',
  );
  const detailFields = /const sessionDetailFields = `(?<fields>[\s\S]*?)`/.exec(serviceSource);
  const normalizer = /function normalizeSessionDetail[\s\S]*?\n}/.exec(serviceSource);

  assert.match(detailFields?.groups?.fields ?? '', /\bforkedFromSessionId\b/);
  assert.match(
    normalizer?.[0] ?? '',
    /forkedFromSessionId: session\.forkedFromSessionId \?\? null/,
  );
  assert.match(
    detailSource,
    /<q-item v-if="session\?\.forkedFromSessionId">[\s\S]*?<q-item-label caption>Fork 来源<\/q-item-label>/,
  );
  assert.match(detailSource, /\{\{ session\.forkedFromSessionId \}\}/);
  assert.match(
    detailSource,
    /name: 'session-detail',[\s\S]*?params: \{ id: session\.forkedFromSessionId \}/,
  );
  assert.doesNotMatch(detailSource, /label="查看 Fork 来源"/);
});

test('session detail renders current node information only in workflow mode', () => {
  const source = readFileSync(new URL('../src/components/SessionDetailView.vue', import.meta.url), 'utf8');

  assert.match(
    source,
    /<q-item v-if="session\?\.mode === 'workflow'">\s*<q-item-section>\s*<q-item-label caption>当前节点<\/q-item-label>/,
  );
});

test('session detail information renders the complete user requirement as its title', () => {
  const source = readFileSync(new URL('../src/components/SessionDetailView.vue', import.meta.url), 'utf8');

  assert.match(
    source,
    /<q-item-label caption>标题<\/q-item-label>\s*<q-item-label class="session-requirement-title">\s*\{\{ session\?\.summary \?\? '会话详情' \}\}/,
  );
  assert.match(
    source,
    /\.session-requirement-title\s*\{[^}]*overflow-wrap:\s*anywhere;[^}]*white-space:\s*pre-wrap;/s,
  );
});
