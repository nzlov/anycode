import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  codexCommandResultBody,
  codexMessageImages,
  compactEventPayload,
  mergeSessionEvents,
  prepareTerminalOutput,
  renderMarkdown,
} from '../src/services/sessionEventPresentation.js';

test('codexMessageImages preserves response_item user images', () => {
  assert.deepEqual(
    codexMessageImages({
      content: [
        { type: 'input_text', text: 'inspect this' },
        { type: 'input_image', image_url: 'data:image/png;base64,AAAA', detail: 'high' },
      ],
    }),
    [{ src: 'data:image/png;base64,AAAA', detail: 'high' }],
  );
  assert.deepEqual(
    codexMessageImages({
      output: [
        { type: 'input_text', text: 'captured' },
        { type: 'input_image', image_url: 'data:image/jpeg;base64,BBBB', detail: 'low' },
      ],
    }),
    [{ src: 'data:image/jpeg;base64,BBBB', detail: 'low' }],
  );
});

test('compactEventPayload preserves nested values from unknown transcript records', () => {
  assert.equal(
    compactEventPayload({ value: 'top-level', details: { nested: true }, codexEventId: 'ignored' }),
    'value: top-level · details: {"nested":true}',
  );
});

test('mergeSessionEvents keeps command and result in one tool entry with complete command text', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'git ls-tree -r --name-only HEAD | head -300'",
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

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].title, 'Shell git ls-tree -r --name-only HEAD | head -300');
  assert.equal(
    merged[0].body,
    'web/src/pages/SessionDetailPage.vue\nweb/src/components/SessionEventMessage.vue',
  );
});

test('mergeSessionEvents combines generic tool input and output by tool call id', () => {
  const merged = mergeSessionEvents([
    {
      id: 'tool-start',
      kind: 'tool',
      title: 'mcp__playwright.browser_resize',
      body: '{"width":1440,"height":1000}',
      toolCallId: 'call-1',
      toolPhase: 'started',
      createdAt: '2026-07-09T08:32:15Z',
      time: '08:32',
    },
    {
      id: 'tool-result',
      kind: 'tool',
      title: '工具结果',
      body: 'browser unavailable',
      toolCallId: 'call-1',
      toolPhase: 'completed',
      createdAt: '2026-07-09T08:32:19Z',
      time: '08:32',
    },
  ]);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].title, 'mcp__playwright.browser_resize');
  assert.equal(merged[0].body, '输入\n{"width":1440,"height":1000}\n\n输出\nbrowser unavailable');
});

test('mergeSessionEvents keeps structured tool result images', () => {
  const merged = mergeSessionEvents([
    {
      id: 'tool-start',
      kind: 'tool',
      title: 'image tool',
      body: 'capture screenshot',
      toolCallId: 'call-image',
      toolPhase: 'started',
      createdAt: '2026-07-10T01:00:00Z',
      time: '01:00',
    },
    {
      id: 'tool-result',
      kind: 'tool',
      title: 'image tool',
      body: 'captured',
      images: [{ src: 'data:image/png;base64,AAAA', detail: 'high' }],
      toolCallId: 'call-image',
      toolPhase: 'completed',
      createdAt: '2026-07-10T01:00:01Z',
      time: '01:00',
    },
  ]);

  assert.equal(merged.length, 1);
  assert.deepEqual(merged[0].images, [{ src: 'data:image/png;base64,AAAA', detail: 'high' }]);
});

test('mergeSessionEvents pairs command and result across non-tool status events', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'git ls-tree -r --name-only HEAD | head -300'",
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

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-1');
  assert.equal(merged[0].title, 'Shell git ls-tree -r --name-only HEAD | head -300');
  assert.equal(merged[0].body, 'web/src/pages/SessionDetailPage.vue');
  assert.equal(merged[1].id, 'status-1');
});

test('mergeSessionEvents normalizes shell wrapper commands with escaped spaces', () => {
  const events = [
    {
      id: 'run-1',
      kind: 'tool',
      title: '执行命令',
      body: '/bin/bash -lc \'git diff -- "docs/plan/a file.md"\'',
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged[0].title, 'Shell git diff -- "docs/plan/a file.md"');
  assert.equal(merged[0].body, '');
});

test('mergeSessionEvents does not pair a command with a mismatched command result', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      command: "/bin/bash -lc 'npm test'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-b',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nbuilt',
      command: "/bin/bash -lc 'npm run build'",
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].title, 'Shell npm test');
  assert.equal(merged[0].body, '');
  assert.equal(merged[1].title, 'Shell npm run build');
  assert.equal(merged[1].body, 'built');
});

test('mergeSessionEvents pairs interleaved shell commands with their own results', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      command: "/bin/bash -lc 'npm test'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'run-b',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm run build'",
      command: "/bin/bash -lc 'npm run build'",
      createdAt: '2026-07-07T01:00:00.100Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-a',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nok',
      command: "/bin/bash -lc 'npm test'",
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-b',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nbuilt',
      command: "/bin/bash -lc 'npm run build'",
      createdAt: '2026-07-07T01:00:02Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-a');
  assert.equal(merged[0].title, 'Shell npm test');
  assert.equal(merged[0].body, 'ok');
  assert.equal(merged[1].id, 'run-b');
  assert.equal(merged[1].title, 'Shell npm run build');
  assert.equal(merged[1].body, 'built');
});

test('mergeSessionEvents pairs same command concurrency by tool call id', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-a',
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'run-b',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-b',
      createdAt: '2026-07-07T01:00:00.100Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-a',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nfirst',
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-a',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-b',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nsecond',
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-b',
      createdAt: '2026-07-07T01:00:02Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-a');
  assert.equal(merged[0].body, 'first');
  assert.equal(merged[1].id, 'run-b');
  assert.equal(merged[1].body, 'second');
});

test('mergeSessionEvents does not fall back to command text when result tool call id is unknown', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-a',
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-unknown-call',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\norphan',
      command: "/bin/bash -lc 'npm test'",
      toolCallId: 'call-b',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:01',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-a');
  assert.equal(merged[0].body, '');
  assert.equal(merged[1].id, 'result-unknown-call');
  assert.equal(merged[1].title, 'Shell npm test');
  assert.equal(merged[1].body, 'orphan');
});

test('mergeSessionEvents pairs an unlabelled result with the nearest previous command', () => {
  const events = [
    {
      id: 'run-a',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm test'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'run-b',
      kind: 'tool',
      title: '执行命令',
      body: "/bin/bash -lc 'npm run build'",
      createdAt: '2026-07-07T01:00:00.100Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
    {
      id: 'result-unknown',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\nok',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 2);
  assert.equal(merged[0].id, 'run-a');
  assert.equal(merged[0].body, '');
  assert.equal(merged[1].id, 'run-b');
  assert.equal(merged[1].title, 'Shell npm run build');
  assert.equal(merged[1].body, 'ok');
});

test('mergeSessionEvents keeps the command entry id stable when a live result arrives', () => {
  const command = {
    id: 'run-live',
    kind: 'tool',
    title: '执行命令',
    body: "/bin/bash -lc 'go test ./...'",
    command: "/bin/bash -lc 'go test ./...'",
    createdAt: '2026-07-07T01:00:00Z',
    time: '01:00',
    rawType: 'process.codex_event',
  };
  const result = {
    id: 'result-live',
    kind: 'tool',
    title: '命令结果',
    body: '命令完成\nok',
    command: "/bin/bash -lc 'go test ./...'",
    createdAt: '2026-07-07T01:00:01Z',
    time: '01:01',
    rawType: 'process.codex_event',
  };

  const beforeResult = mergeSessionEvents([command]);
  const afterResult = mergeSessionEvents([command, result]);

  assert.equal(beforeResult.length, 1);
  assert.equal(beforeResult[0].id, 'run-live');
  assert.equal(beforeResult[0].body, '');
  assert.equal(afterResult.length, 1);
  assert.equal(afterResult[0].id, 'run-live');
  assert.equal(afterResult[0].body, 'ok');
  assert.equal(afterResult[0].time, '01:01');
});

test('mergeSessionEvents preserves unmatchable unlabelled result title', () => {
  const events = [
    {
      id: 'result-orphan',
      kind: 'tool',
      title: '命令结果',
      body: '命令完成\norphan',
      createdAt: '2026-07-07T01:00:01Z',
      time: '01:01',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].id, 'result-orphan');
  assert.equal(merged[0].title, '命令结果');
  assert.equal(merged[0].body, 'orphan');
});

test('mergeSessionEvents keeps file change id stable and updates completed changes', () => {
  const started = {
    id: 'file-started',
    kind: 'file_change',
    title: '修改文件 internal/infra/gitcli/git_test.go',
    body: '',
    fileChangeId: 'item_3',
    fileChanges: [{ kind: 'update', path: 'internal/infra/gitcli/git_test.go' }],
    createdAt: '2026-07-08T06:26:49Z',
    time: '06:26',
    rawType: 'process.codex_event',
  };
  const completed = {
    id: 'file-completed',
    kind: 'file_change',
    title: '修改文件 internal/infra/gitcli/git_test.go',
    body: '',
    fileChangeId: 'item_3',
    fileChanges: [
      {
        kind: 'update',
        path: 'internal/infra/gitcli/git_test.go',
        unifiedDiff: '@@ -1 +1 @@\n-old\n+new',
      },
    ],
    createdAt: '2026-07-08T06:26:50Z',
    time: '06:26',
    rawType: 'process.codex_event',
  };

  const beforeComplete = mergeSessionEvents([started]);
  const afterComplete = mergeSessionEvents([started, completed]);

  assert.equal(beforeComplete.length, 1);
  assert.equal(beforeComplete[0].id, 'file-started');
  assert.equal(beforeComplete[0].fileChanges[0].path, 'internal/infra/gitcli/git_test.go');
  assert.equal(afterComplete.length, 1);
  assert.equal(afterComplete[0].id, 'file-started');
  assert.equal(afterComplete[0].time, '06:26');
  assert.equal(afterComplete[0].fileChanges[0].path, 'internal/infra/gitcli/git_test.go');
  assert.equal(afterComplete[0].fileChanges[0].unifiedDiff, '@@ -1 +1 @@\n-old\n+new');
});

test('mergeSessionEvents shows file paths in multi-file change title', () => {
  const events = [
    {
      id: 'file-started',
      kind: 'file_change',
      title: '修改文件 a.go, b.go',
      body: '',
      fileChangeId: 'item_4',
      fileChanges: [
        { kind: 'update', path: 'a.go' },
        { kind: 'update', path: 'b.go' },
      ],
      createdAt: '2026-07-08T06:26:49Z',
      time: '06:26',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].title, '修改文件 a.go, b.go');
});

test('file change title keeps paths visible for multi-file changes', () => {
  const source = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');

  assert.match(source, /visiblePaths = changes\.slice\(0, 3\)\.map\(\(change\) => change\.path\)/);
  assert.doesNotMatch(source, /return `修改 \$\{changes\.length\} 个文件`/);
});

test('reasoning codex items render as thought events', () => {
  const source = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');

  assert.match(source, /if \(itemType === 'reasoning'\) return 'thought';/);
  assert.match(source, /if \(type === 'reasoning'\) return `思考\$\{suffix\}`;/);
});

test('web search codex items render as tool events', () => {
  const source = readFileSync(new URL('../src/services/sessions.ts', import.meta.url), 'utf8');

  assert.match(source, /'web_search'/);
  assert.match(source, /if \(type === 'web_search'\) return '网页搜索';/);
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

  assert.equal(body, 'first line\nsecond line');
});

test('codexCommandResultBody falls back when aggregated output is empty', () => {
  const body = codexCommandResultBody({
    type: 'command_execution',
    exit_code: 0,
    aggregated_output: '',
    output: 'fallback output',
  });

  assert.equal(body, 'fallback output');
});

test('mergeSessionEvents keeps completed command ANSI for terminal rendering', () => {
  const events = [
    {
      id: 'completed-1',
      kind: 'tool',
      title: '命令结果',
      body: '\u001b[38;2;25;118;210m \u001b[39m\u001b[38;2;31;115;204m█\u001b[39m38;2;34;113;201m█\nDone',
      command: "/bin/bash -lc 'git diff --stat'",
      createdAt: '2026-07-07T01:00:00Z',
      time: '01:00',
      rawType: 'process.codex_event',
    },
  ];

  const merged = mergeSessionEvents(events);

  assert.equal(merged.length, 1);
  assert.equal(merged[0].title, 'Shell git diff --stat');
  assert.equal(
    merged[0].body,
    '\u001b[38;2;25;118;210m \u001b[39m\u001b[38;2;31;115;204m█\u001b[39m38;2;34;113;201m█\nDone',
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
