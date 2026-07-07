import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  codexCommandResultBody,
  mergeShellEvents,
  renderMarkdown,
} from '../src/services/sessionEventPresentation.js';

test('mergeShellEvents keeps command and result in one tool entry with complete command text', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: "[redacted_path] -lc 'git ls-tree -r --name-only HEAD | head -300'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-1',
      kind: 'tool',
      title: '命令结果',
      body: 'web/src/pages/SessionDetailPage.vue\nweb/src/components/SessionEventMessage.vue',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeShellEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(
    merged[0].title,
    "Shell [redacted_path] -lc 'git ls-tree -r --name-only HEAD | head -300'",
  );
  assert.match(merged[0].body, /命令\n\[redacted_path\] -lc/);
  assert.match(merged[0].body, /结果\nweb\/src\/pages\/SessionDetailPage\.vue/);
});

test('mergeShellEvents pairs command and result across non-tool status events', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: "[redacted_path] -lc 'git ls-tree -r --name-only HEAD | head -300'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'status-1',
      kind: 'status',
      title: '运行中',
      body: '会话正在运行。',
      createdAt: '2026-07-07T01:00:00.500Z',
      time: '01:00',
      rawType: 'session.running',
    },
    {
      id: 'result-1',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nweb/src/pages/SessionDetailPage.vue',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeShellEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-1:result-1');
  assert.equal(
    merged[0].title,
    "Shell [redacted_path] -lc 'git ls-tree -r --name-only HEAD | head -300'",
  );
  assert.match(merged[0].body, /结果\n命令完成\nweb\/src\/pages\/SessionDetailPage\.vue/);
  assert.equal(merged[1].id, 'status-1');
});

test('renderMarkdown formats assistant markdown and escapes raw html', () => {
  const html = renderMarkdown('**结论**\n\n- `npm test`\n\n<script>alert(1)</script>');

  assert.match(html, /<strong>结论<\/strong>/);
  assert.match(html, /<ul>/);
  assert.match(html, /<code>npm test<\/code>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.equal(html.includes('<script>'), false);
});

test('renderMarkdown keeps single line breaks inside assistant paragraphs', () => {
  const html = renderMarkdown('第一行\n第二行');

  assert.equal(html, '<p>第一行<br>第二行</p>');
});

test('codexCommandResultBody keeps output when aggregated output is absent', () => {
  const body = codexCommandResultBody({
    type: 'command_execution',
    exit_code: 0,
    output: 'first line\nsecond line',
  });

  assert.equal(body, '命令完成\nfirst line\nsecond line');
});
