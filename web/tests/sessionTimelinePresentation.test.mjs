import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  formatTokenCount,
  sessionTextPresentation,
  statusLabel,
  toolLabel,
} from '../src/services/sessionTimelinePresentation.ts';

test('statusLabel describes durable questions suspension events', () => {
  assert.equal(
    statusLabel({ code: 'process.suspended_for_user', level: 'info' }),
    '已挂起等待回答',
  );
  assert.equal(
    statusLabel({ code: 'session.answer_resume_queued', level: 'info' }),
    '答案已提交，等待恢复',
  );
  assert.equal(statusLabel({ code: 'question.cancelled', level: 'info' }), '待回答问题已取消');
  assert.equal(statusLabel({ code: 'session.waiting_approval', level: 'warning' }), '待审批');
});

test('formatTokenCount converts token counts to compact decimal units', () => {
  assert.equal(formatTokenCount(999), '999');
  assert.equal(formatTokenCount(1_000), '1K');
  assert.equal(formatTokenCount(12_500), '12.5K');
  assert.equal(formatTokenCount(999_999), '1M');
  assert.equal(formatTokenCount(2_000_000), '2M');
  assert.equal(formatTokenCount(1_500_000_000), '1.5B');
});

test('toolLabel displays the structured qualified tool name', () => {
  const content = {
    qualifiedName: 'tools.update_plan',
    category: 'generic',
    input: { format: 'plain', text: 'const result = await tools.update_plan({ plan: [] });' },
    output: { format: 'plain', text: '' },
    images: [],
  };

  assert.equal(toolLabel(content), 'tools.update_plan');
  assert.equal(toolLabel({ ...content, qualifiedName: 'mcp.exec' }), 'mcp.exec');
  assert.equal(toolLabel({ ...content, qualifiedName: 'questions' }), '向用户提问');
});

test('sessionTextPresentation folds injected runtime context', () => {
  const agents =
    '# AGENTS.md instructions for /workspace/project\n\n<INSTRUCTIONS>rules</INSTRUCTIONS>';
  const environment =
    '<environment_context>\n  <cwd>/workspace/project</cwd>\n</environment_context>';
  const combined = `${agents}\n${environment}`;

  assert.deepEqual(sessionTextPresentation('user', agents), {
    text: '',
    foldedLabel: '运行上下文',
    foldedText: agents,
  });
  assert.deepEqual(sessionTextPresentation('user', environment), {
    text: '',
    foldedLabel: '运行上下文',
    foldedText: environment,
  });
  assert.deepEqual(sessionTextPresentation('user', combined), {
    text: '',
    foldedLabel: '运行上下文',
    foldedText: combined,
  });
});

test('sessionTextPresentation preserves text after a closed runtime context block', () => {
  const agentsWithUserText =
    '# AGENTS.md instructions for /workspace/project\n\n<INSTRUCTIONS>rules</INSTRUCTIONS>\n请继续实现功能';
  const environmentWithUserText =
    '<environment_context><cwd>/workspace/project</cwd></environment_context>\n请继续实现功能';

  for (const text of [agentsWithUserText, environmentWithUserText]) {
    assert.deepEqual(sessionTextPresentation('user', text), {
      text,
      foldedLabel: '',
      foldedText: '',
    });
  }
});

test('sessionTextPresentation preserves complete context blocks submitted by the user', () => {
  const agents =
    '# AGENTS.md instructions for /workspace/project\n\n<INSTRUCTIONS>review this file</INSTRUCTIONS>';
  const environment = '<environment_context><cwd>/workspace/project</cwd></environment_context>';

  for (const text of [agents, environment]) {
    assert.deepEqual(sessionTextPresentation('user', text, [text]), {
      text,
      foldedLabel: '',
      foldedText: '',
    });
  }
});

test('sessionTextPresentation rejects nested runtime context markers', () => {
  const nestedAgents =
    '# AGENTS.md instructions for /workspace/project\n\n<INSTRUCTIONS><INSTRUCTIONS>rules</INSTRUCTIONS></INSTRUCTIONS>';
  const nestedEnvironment =
    '<environment_context><environment_context></environment_context></environment_context>';

  for (const text of [nestedAgents, nestedEnvironment]) {
    assert.deepEqual(sessionTextPresentation('user', text), {
      text,
      foldedLabel: '',
      foldedText: '',
    });
  }
});

test('sessionTextPresentation does not parse developer instructions from user text', () => {
  const text = '继续处理\n\nAnyCode 提供 `questions` App Server 动态工具。';
  assert.deepEqual(sessionTextPresentation('user', text, ['继续处理']), {
    text,
    foldedLabel: '',
    foldedText: '',
  });
});

test('sessionTextPresentation leaves unmatched and assistant messages unchanged', () => {
  const incompleteMarker = 'AnyCode 提供 questions 工具，但这是用户自己的说明。';
  const incompleteGuidance =
    '用户正文\n\nAnyCode 提供 `questions` App Server 动态工具，但没有完整的固定说明。';
  const quotedGuidanceFragments =
    '请分析 AnyCode 提供 `questions` App Server 动态工具的实现。后文还需要比较 `request_user_input`，并检查不得删除、移动、重建或清理当前工作树的约束。';
  const assistant = '# AGENTS.md instructions for example';

  assert.deepEqual(sessionTextPresentation('user', incompleteMarker), {
    text: incompleteMarker,
    foldedLabel: '',
    foldedText: '',
  });
  assert.deepEqual(sessionTextPresentation('user', incompleteGuidance), {
    text: incompleteGuidance,
    foldedLabel: '',
    foldedText: '',
  });
  assert.deepEqual(sessionTextPresentation('user', quotedGuidanceFragments), {
    text: quotedGuidanceFragments,
    foldedLabel: '',
    foldedText: '',
  });
  assert.deepEqual(sessionTextPresentation('assistant', assistant), {
    text: assistant,
    foldedLabel: '',
    foldedText: '',
  });
});
