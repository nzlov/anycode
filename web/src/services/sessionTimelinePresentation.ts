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
  if (content.qualifiedName === 'questions') return '向用户提问';
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

export function sessionTextPresentation(
  role: string,
  text: string,
  knownUserPrompts: readonly string[] = [],
): SessionTextPresentation {
  if (role !== 'user') {
    return { text, foldedLabel: '', foldedText: '' };
  }

  if (knownUserPrompts.some((prompt) => prompt.trim() === text.trim())) {
    return { text, foldedLabel: '', foldedText: '' };
  }

  if (isCompleteInjectedContext(text)) {
    return { text: '', foldedLabel: '运行上下文', foldedText: text.trim() };
  }

  return { text, foldedLabel: '', foldedText: '' };
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
