import assert from 'node:assert/strict';
import { test } from 'node:test';

import {
  formatTokenCount,
  sessionTextPresentation,
} from '../src/services/sessionTimelinePresentation.ts';

test('formatTokenCount converts token counts to compact decimal units', () => {
  assert.equal(formatTokenCount(999), '999');
  assert.equal(formatTokenCount(1_000), '1K');
  assert.equal(formatTokenCount(12_500), '12.5K');
  assert.equal(formatTokenCount(999_999), '1M');
  assert.equal(formatTokenCount(2_000_000), '2M');
  assert.equal(formatTokenCount(1_500_000_000), '1.5B');
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

test('sessionTextPresentation separates AnyCode guidance from user text', () => {
  const answerUserGuidance =
    'AnyCode 提供 `answer_user` MCP 工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `answer_user` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `answer_user`。';
  const worktreeGuidance =
    '当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；若必须手动合并，请使用当前卡片分支名执行非 fast-forward merge，并保留 Git 默认合并提交信息，以便工作树缺失时从基础分支日志恢复 Diff；卡片关闭时由 AnyCode 负责清理仍存在的工作树。';
  const guidance = `${answerUserGuidance}\n\n${worktreeGuidance}`;

  assert.deepEqual(
    sessionTextPresentation('user', `合并到基础分支并推送\n\n${guidance}`, [
      '合并到基础分支并推送',
    ]),
    {
      text: '合并到基础分支并推送',
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );
  assert.deepEqual(
    sessionTextPresentation('user', `继续处理\n\n${worktreeGuidance}`, ['继续处理']),
    {
      text: '继续处理',
      foldedLabel: 'AnyCode 附加说明',
      foldedText: worktreeGuidance,
    },
  );
  const quotedMarker =
    '请解释下面这句为何存在：\n\nAnyCode 提供 `answer_user` MCP 工具，但这里仍是用户正文。';
  assert.deepEqual(
    sessionTextPresentation('user', `${quotedMarker}\n\n${guidance}`, [quotedMarker]),
    {
      text: quotedMarker,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );

  const exactUserQuote = `请解释：\n\n${answerUserGuidance}`;
  assert.deepEqual(sessionTextPresentation('user', exactUserQuote, [exactUserQuote]), {
    text: exactUserQuote,
    foldedLabel: '',
    foldedText: '',
  });

  const attachments = 'Attached files available on disk:\n- /workspace/request.txt';
  assert.deepEqual(
    sessionTextPresentation('user', `${exactUserQuote}\n\n${attachments}`, [exactUserQuote]),
    {
      text: exactUserQuote,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: attachments,
    },
  );
  assert.deepEqual(
    sessionTextPresentation('user', `继续处理\n\n${guidance}\n\n${attachments}`, ['继续处理']),
    {
      text: '继续处理',
      foldedLabel: 'AnyCode 附加说明',
      foldedText: `${guidance}\n\n${attachments}`,
    },
  );

  const workflow =
    'Validate build\n\nUser requirement:\nship it\n\nWorkflow input params JSON:\n```json\n{}\n```';
  assert.deepEqual(
    sessionTextPresentation('user', `${workflow}\n\n${guidance}`, ['ship it'], true),
    {
      text: workflow,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );

  const rebuilt = [
    '无法复用已有 Codex 会话，请基于以下上下文复查当前状态并继续处理。',
    '原始需求：\n分析会话内容',
    '追加描述：\n只前端处理隐藏或折叠可以吗？',
    '追加描述：\n开始吧',
  ].join('\n\n');
  assert.deepEqual(
    sessionTextPresentation(
      'user',
      `${rebuilt}\n\n${guidance}`,
      ['分析会话内容', '只前端处理隐藏或折叠可以吗？', '开始吧'],
    ),
    {
      text: rebuilt,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );

  assert.deepEqual(
    sessionTextPresentation(
      'user',
      `${rebuilt}\n\n${guidance}`,
      ['分析会话内容', '只前端处理隐藏或折叠可以吗？', '开始吧', '后来新增的描述'],
    ),
    {
      text: rebuilt,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );

  const rebuiltWorkflow = `${rebuilt}\n\n当前流程节点提示词：\n复查当前实现`;
  assert.deepEqual(
    sessionTextPresentation(
      'user',
      `${rebuiltWorkflow}\n\n${guidance}`,
      ['分析会话内容', '只前端处理隐藏或折叠可以吗？', '开始吧'],
    ),
    {
      text: rebuiltWorkflow,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: guidance,
    },
  );
});

test('sessionTextPresentation leaves unmatched and assistant messages unchanged', () => {
  const incompleteMarker = 'AnyCode 提供 answer_user 工具，但这是用户自己的说明。';
  const incompleteGuidance =
    '用户正文\n\nAnyCode 提供 `answer_user` MCP 工具，但没有完整的固定说明。';
  const quotedGuidanceFragments =
    '请分析 AnyCode 提供 `answer_user` MCP 工具的实现。后文还需要比较 `request_user_input`，并检查不得删除、移动、重建或清理当前工作树的约束。';
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
