import type {
  SessionStatusContent,
  SessionTimelinePhase,
  SessionTimelineFileChange,
  SessionToolContent,
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
  'session.queued': '排队中',
  'session.starting': '启动中',
  'session.started': '已启动',
  'session.running': '运行中',
  'session.waiting_user': '待回答',
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

export function statusLabel(content: SessionStatusContent) {
  return statusLabels[content.code] ?? content.code.replaceAll('.', ' ');
}

export function statusIcon(content: SessionStatusContent) {
  if (content.level === 'error') return 'error_outline';
  if (content.level === 'warning') return 'warning_amber';
  return 'info_outline';
}

export function statusColor(content: SessionStatusContent) {
  if (content.level === 'error') return 'negative';
  if (content.level === 'warning') return 'warning';
  return 'blue-grey';
}

const phasePresentation: Record<
  SessionTimelinePhase,
  { icon: string; color: string; label: string }
> = {
  standalone: { icon: 'info_outline', color: 'blue-grey', label: '独立事件' },
  started: { icon: 'pending', color: 'primary', label: '执行中' },
  progress: { icon: 'pending', color: 'primary', label: '执行中' },
  completed: { icon: 'check_circle_outline', color: 'positive', label: '已完成' },
  failed: { icon: 'error_outline', color: 'negative', label: '执行失败' },
  cancelled: { icon: 'cancel', color: 'grey-7', label: '已取消' },
};

export function timelinePhaseIcon(phase: SessionTimelinePhase) {
  return phasePresentation[phase].icon;
}

export function timelinePhaseColor(phase: SessionTimelinePhase) {
  return phasePresentation[phase].color;
}

export function timelinePhaseLabel(phase: SessionTimelinePhase) {
  return phasePresentation[phase].label;
}

export function toolLabel(content: SessionToolContent) {
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

export function fileChangeLabel(changes: SessionTimelineFileChange[]) {
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

export function stripUnsupportedAnsiControls(value: string) {
  const oscSequence = new RegExp(
    String.raw`(?:\u001b\]|\u009d)[\s\S]*?(?:\u0007|\u001b\\|\u009c|$)`,
    'g',
  );
  return value.replace(oscSequence, '');
}
