import type {
  TranscriptStatusContent,
  TranscriptPhase,
  TranscriptFileChange,
  TranscriptToolContent,
} from '@/services/sessionTimeline';

const statusLabels: Record<string, string> = {
  'thread.started': '线程已创建',
  'task.started': '任务开始',
  'task.completed': '任务完成',
  'turn.started': '开始执行',
  'turn.completed': '本轮完成',
  'turn.aborted': '本轮已中止',
  'context.compacted': '上下文已压缩',
  'turn.context': '回合上下文',
  'world.state': '工作区状态',
  'process.exit': '进程退出',
  'process.exited': '进程退出',
  'process.suspended_for_user': '已挂起等待回答',
  'question.pending': '收到待回答问题',
  'question.cancelled': '待回答问题已取消',
  'session.queued': '排队中',
  'session.answer_resume_queued': '答案已提交，等待恢复',
  'session.starting': '启动中',
  'session.started': '已启动',
  'session.running': '运行中',
  'session.waiting_user': '待回答',
  'session.waiting_approval': '待审批',
  'session.stopping': '停止中',
  'session.stopped': '已停止',
  'session.failed': '会话失败',
  'session.resume_failed': '恢复失败',
  'session.completed': '已完成',
  'workflow.blocked': '流程阻塞',
  error: 'Codex 错误',
  invalid_json: '无效事件',
};

export function timelineTime(value: string) {
  if (!value) return '';
  return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(
    new Date(value),
  );
}

export function statusLabel(content: TranscriptStatusContent) {
  return statusLabels[content.code] ?? content.code.replaceAll('.', ' ');
}

export function statusIcon(content: TranscriptStatusContent) {
  if (content.level === 'error') return 'error_outline';
  if (content.level === 'warning') return 'warning_amber';
  return 'info_outline';
}

export function statusColor(content: TranscriptStatusContent) {
  if (content.level === 'error') return 'negative';
  if (content.level === 'warning') return 'warning';
  return 'blue-grey';
}

const phasePresentation: Record<TranscriptPhase, { icon: string; color: string; label: string }> = {
  standalone: { icon: 'info_outline', color: 'blue-grey', label: '独立事件' },
  started: { icon: 'pending', color: 'primary', label: '执行中' },
  progress: { icon: 'pending', color: 'primary', label: '执行中' },
  completed: { icon: 'check_circle_outline', color: 'positive', label: '已完成' },
  failed: { icon: 'error_outline', color: 'negative', label: '执行失败' },
  cancelled: { icon: 'cancel', color: 'grey-7', label: '已取消' },
};

export function timelinePhaseIcon(phase: TranscriptPhase) {
  return phasePresentation[phase].icon;
}

export function timelinePhaseColor(phase: TranscriptPhase) {
  return phasePresentation[phase].color;
}

export function timelinePhaseLabel(phase: TranscriptPhase) {
  return phasePresentation[phase].label;
}

export function toolLabel(content: TranscriptToolContent) {
  if (content.qualifiedName) return content.qualifiedName;
  const labels: Record<string, string> = {
    web_search: '网页搜索',
    tool_search: '搜索工具',
    mcp: 'MCP 工具',
    custom: '自定义工具',
    generic: '工具调用',
  };
  return labels[content.category] ?? '工具调用';
}

export function fileChangeLabel(changes: TranscriptFileChange[]) {
  if (changes.length === 0) return '修改文件';
  if (changes.length === 1) return `修改文件 ${changes[0]?.path ?? ''}`.trim();
  return `修改 ${changes.length} 个文件`;
}

export function fileChangeKindLabel(kind: string) {
  const labels: Record<string, string> = {
    added: '新增',
    modified: '修改',
    deleted: '删除',
    renamed: '重命名',
  };
  return labels[kind] ?? kind;
}

export function formatDuration(durationMs: number | null) {
  if (durationMs === null) return '';
  if (durationMs < 1000) return `${durationMs} ms`;
  return `${(durationMs / 1000).toFixed(durationMs < 10000 ? 1 : 0)} s`;
}

const compactTokenCountFormatter = new Intl.NumberFormat('en-US', {
  notation: 'compact',
  maximumFractionDigits: 1,
});

export function formatTokenCount(value: number) {
  return compactTokenCountFormatter.format(value);
}

export interface SessionTextPresentation {
  text: string;
  foldedLabel: string;
  foldedText: string;
}

const agentsContextPrefix = '# AGENTS.md instructions for ';
const agentsInstructionsOpen = '<INSTRUCTIONS>';
const agentsInstructionsClose = '</INSTRUCTIONS>';
const environmentContextPrefix = '<environment_context>';
const environmentContextClose = '</environment_context>';

// GLUE: These exact suffixes mirror backend-injected guidance for frontend-only folding.
// Remove them when timeline messages expose their user/injected provenance.
const answerUserPromptGuidance =
  'AnyCode 提供 `answer_user` MCP 工具，可用于向用户提出选项问题。若需求、验收标准、执行取舍或下一步不确定，请使用 `answer_user` 咨询用户；如果上下文足够明确，请直接继续执行，不要无意义打断用户。`request_user_input` 不是 AnyCode 会话内的用户提问工具，可能只属于外层平台或特定计划模式；即使你在说明中看到它，也不要使用 `request_user_input` 来代替 AnyCode 的 `answer_user`。';
const worktreePromptGuidance =
  '当前工作目录是 AnyCode 管理的卡片工作树。不得删除、移动、重建或清理当前工作树，也不得执行会移除该工作树的命令；若必须手动合并，请使用当前卡片分支名执行非 fast-forward merge，并保留 Git 默认合并提交信息，以便工作树缺失时从基础分支日志恢复 Diff；卡片关闭时由 AnyCode 负责清理仍存在的工作树。';
const artifactPromptGuidance =
  '本卡片生成的图片、截图、PDF、音视频、压缩包和其他临时文件统一写入环境变量 `ANYCODE_ARTIFACT_DIR` 指向的目录。需要生图时直接使用 Codex 可用的图片生成能力，并将结果保存到该目录；不要把生成物写入项目工作树。';
const legacyArtifactPromptGuidance =
  '本卡片生成的图片、截图、PDF、音视频、压缩包和其他产物统一写入环境变量 `ANYCODE_ARTIFACT_DIR` 指向的目录。需要生图时直接使用 Codex 可用的图片生成能力，并将结果保存到该目录；不要把生成物写入项目工作树。';
const anyCodeGuidanceSuffixes = [
  ...[artifactPromptGuidance, legacyArtifactPromptGuidance].flatMap((guidance) => [
    `${guidance}\n\n${answerUserPromptGuidance}\n\n${worktreePromptGuidance}`,
    `${guidance}\n\n${answerUserPromptGuidance}`,
    guidance,
  ]),
  `${answerUserPromptGuidance}\n\n${worktreePromptGuidance}`,
  answerUserPromptGuidance,
  worktreePromptGuidance,
];
const attachedFilesMarker = '\n\nAttached files available on disk:\n';
const workflowRequirementPrefix = 'User requirement:\n';
const workflowInputMarker = '\n\nWorkflow input params JSON:\n';
const rebuiltPromptNotice = '无法复用已有 Codex 会话，请基于以下上下文复查当前状态并继续处理。';
const originalRequirementPrefix = '原始需求：\n';
const appendedRequirementPrefix = '追加描述：\n';
const currentNodePromptPrefix = '当前流程节点提示词：\n';

export function sessionTextPresentation(
  role: string,
  text: string,
  knownUserPrompts: readonly string[] = [],
  workflowPrompt = false,
): SessionTextPresentation {
  if (role !== 'user') {
    return { text, foldedLabel: '', foldedText: '' };
  }

  const normalizedKnownPrompts = knownUserPrompts.map((prompt) => prompt.trim()).filter(Boolean);
  const knownPromptSet = new Set(normalizedKnownPrompts);
  if (knownPromptSet.has(text.trim())) {
    return { text, foldedLabel: '', foldedText: '' };
  }

  if (isCompleteInjectedContext(text)) {
    return { text: '', foldedLabel: '运行上下文', foldedText: text.trim() };
  }

  const textWithoutTrailingWhitespace = text.trimEnd();
  const attachmentOffset = attachedFilesOffset(textWithoutTrailingWhitespace);
  const guidanceSearchText =
    attachmentOffset >= 0
      ? textWithoutTrailingWhitespace.slice(0, attachmentOffset)
      : textWithoutTrailingWhitespace;
  const guidanceOffset = anyCodeGuidanceSuffixes.reduce((firstOffset, suffix) => {
    const marker = `\n\n${suffix}`;
    if (!guidanceSearchText.endsWith(marker)) return firstOffset;
    const offset = guidanceSearchText.length - marker.length;
    if (firstOffset < 0) return offset;
    return Math.min(firstOffset, offset);
  }, -1);
  const userText = text.slice(0, guidanceOffset).trimEnd();
  if (
    guidanceOffset > 0 &&
    isKnownPrompt(userText, normalizedKnownPrompts, knownPromptSet, workflowPrompt)
  ) {
    return {
      text: userText,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: text.slice(guidanceOffset).trim(),
    };
  }
  const attachmentUserText = text.slice(0, attachmentOffset).trimEnd();
  if (
    attachmentOffset > 0 &&
    isKnownPrompt(attachmentUserText, normalizedKnownPrompts, knownPromptSet, workflowPrompt)
  ) {
    return {
      text: attachmentUserText,
      foldedLabel: 'AnyCode 附加说明',
      foldedText: text.slice(attachmentOffset).trim(),
    };
  }

  return { text, foldedLabel: '', foldedText: '' };
}

function attachedFilesOffset(text: string) {
  const offset = text.lastIndexOf(attachedFilesMarker);
  if (offset < 0) return -1;
  const lines = text.slice(offset + attachedFilesMarker.length).split('\n');
  if (lines.length === 0 || lines.some((line) => !line.startsWith('- ') || !line.slice(2).trim())) {
    return -1;
  }
  return offset;
}

function isKnownPrompt(
  text: string,
  normalizedKnownPrompts: readonly string[],
  knownPromptSet: ReadonlySet<string>,
  workflowPrompt: boolean,
) {
  const normalized = text.trim();
  if (knownPromptSet.has(normalized)) return true;
  if (isRebuiltSessionPrompt(normalized, normalizedKnownPrompts)) return true;
  if (!workflowPrompt) return false;

  for (const prompt of knownPromptSet) {
    const requirement = `${workflowRequirementPrefix}${prompt}`;
    let offset = normalized.indexOf(requirement);
    while (offset >= 0) {
      const atSectionBoundary = offset === 0 || normalized.slice(offset - 2, offset) === '\n\n';
      const afterRequirement = offset + requirement.length;
      if (atSectionBoundary && normalized.startsWith(workflowInputMarker, afterRequirement)) {
        return true;
      }
      offset = normalized.indexOf(requirement, offset + requirement.length);
    }
  }
  return false;
}

function isRebuiltSessionPrompt(text: string, knownPrompts: readonly string[]) {
  if (knownPrompts.length === 0) return false;
  const [original, ...appends] = knownPrompts;
  const sections = [rebuiltPromptNotice];
  if (original) sections.push(`${originalRequirementPrefix}${original}`);
  for (let appendCount = 0; appendCount <= appends.length; appendCount += 1) {
    const knownContext = [
      ...sections,
      ...appends.slice(0, appendCount).map((append) => `${appendedRequirementPrefix}${append}`),
    ].join('\n\n');
    if (text === knownContext) return true;
    const nodePromptOffset = `${knownContext}\n\n${currentNodePromptPrefix}`;
    if (text.startsWith(nodePromptOffset) && text.slice(nodePromptOffset.length).trim())
      return true;
  }
  return false;
}

function isCompleteInjectedContext(text: string) {
  const normalized = text.trim();
  if (normalized.startsWith(environmentContextPrefix)) {
    return isCompleteEnvironmentContext(normalized);
  }
  if (!normalized.startsWith(agentsContextPrefix)) return false;

  const instructionsOpen = normalized.indexOf(agentsInstructionsOpen);
  if (instructionsOpen < 0) return false;
  if (
    normalized.indexOf(agentsInstructionsOpen, instructionsOpen + agentsInstructionsOpen.length) >=
    0
  ) {
    return false;
  }
  const instructionsClose = normalized.indexOf(
    agentsInstructionsClose,
    instructionsOpen + agentsInstructionsOpen.length,
  );
  if (instructionsClose < 0) return false;
  if (
    normalized.indexOf(
      agentsInstructionsClose,
      instructionsClose + agentsInstructionsClose.length,
    ) >= 0
  ) {
    return false;
  }

  const remainder = normalized.slice(instructionsClose + agentsInstructionsClose.length).trim();
  return remainder === '' || isCompleteEnvironmentContext(remainder);
}

function isCompleteEnvironmentContext(text: string) {
  if (!text.startsWith(environmentContextPrefix)) return false;
  if (text.indexOf(environmentContextPrefix, environmentContextPrefix.length) >= 0) return false;
  const close = text.indexOf(environmentContextClose, environmentContextPrefix.length);
  if (close < 0) return false;
  if (text.indexOf(environmentContextClose, close + environmentContextClose.length) >= 0) {
    return false;
  }
  return text.slice(close + environmentContextClose.length).trim() === '';
}

export function stripUnsupportedAnsiControls(value: string) {
  const oscSequence = new RegExp(
    String.raw`(?:\u001b\]|\u009d)[\s\S]*?(?:\u0007|\u001b\\|\u009c|$)`,
    'g',
  );
  return value.replace(oscSequence, '');
}
