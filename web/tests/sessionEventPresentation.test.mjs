import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  codexCommandResultBody,
  mergeShellEvents,
  prepareTerminalOutput,
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
  assert.equal(merged[0].title, 'Shell git ls-tree -r --name-only HEAD | head -300');
  assert.match(merged[0].body, /命令\ngit ls-tree -r --name-only HEAD \| head -300/);
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
  assert.equal(merged[0].title, 'Shell git ls-tree -r --name-only HEAD | head -300');
  assert.match(merged[0].body, /结果\n命令完成\nweb\/src\/pages\/SessionDetailPage\.vue/);
  assert.equal(merged[1].id, 'status-1');
});

test('mergeShellEvents normalizes shell wrapper commands with escaped spaces', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: "[redacted_path] -lc 'git diff -- \"docs/plan/a file.md\"'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeShellEvents(events);

  assert.equal(merged[0].title, 'Shell git diff -- "docs/plan/a file.md"');
  assert.equal(merged[0].body, '命令\ngit diff -- "docs/plan/a file.md"');
});

test('mergeShellEvents does not pair a command with a mismatched command result', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "[redacted_path] -lc 'npm test'",
      command: "[redacted_path] -lc 'npm test'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-b',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nbuilt',
      command: "[redacted_path] -lc 'npm run build'",
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeShellEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].title, 'Shell npm test');
  assert.equal(merged[0].body, '命令\nnpm test');
  assert.equal(merged[1].title, 'Shell npm run build');
  assert.match(merged[1].body, /结果\n命令完成\nbuilt/);
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

test('mergeShellEvents keeps completed command ANSI for terminal rendering', () => {
  const events = [
    {
      id: 'completed-1',
      kind: 'tool',
      title: '命令结果',
      body: "命令完成\n\u001b[38;2;25;118;210m \u001b[39m\u001b[38;2;31;115;204m█\u001b[39m38;2;34;113;201m█\nDone",
      command: "[redacted_path] -lc 'git diff --stat'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeShellEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].title, 'Shell git diff --stat');
  assert.equal(
    merged[0].body,
    "命令\ngit diff --stat\n\n结果\n命令完成\n\u001b[38;2;25;118;210m \u001b[39m\u001b[38;2;31;115;204m█\u001b[39m38;2;34;113;201m█\nDone",
  );
  assert.equal(merged[0].body.includes('\u001b[38;2;31;115;204m'), true);
});

test('renderMarkdown preserves a blank line between command result text and model prose', () => {
  const html = renderMarkdown('结果\n命令完成\n\n主人，下一步继续。');

  assert.match(html, /<p>结果<br>命令完成<\/p><p>主人，下一步继续。<\/p>/);
});

test('prepareTerminalOutput keeps valid CSI and recovers visible escape markers', () => {
  const output = prepareTerminalOutput('ok ␛[32mgreen␛[39m \u001b[38;2;31;115;204m█\u001b[39m');

  assert.equal(output, 'ok \u001b[32mgreen\u001b[39m \u001b[38;2;31;115;204m█\u001b[39m');
});

test('prepareTerminalOutput removes orphan SGR fragments after terminal capture damage', () => {
  const output = prepareTerminalOutput('命令完成\n38;2;34;113;201m█\n\u001b[39m38;2;35;114;202m█');

  assert.equal(output, '命令完成\n█\n\u001b[39m█');
});

test('prepareTerminalOutput keeps ordinary semicolon data that only resembles SGR', () => {
  const output = prepareTerminalOutput('value 1;2;3m should stay');

  assert.equal(output, 'value 1;2;3m should stay');
});
