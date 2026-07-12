import type { SessionStatus } from './sessions';

const labels: Record<SessionStatus, string> = {
  created: '待运行',
  queued: '排队中',
  starting: '启动中',
  running: '运行中',
  waiting_user: '待回答',
  waiting_approval: '待审批',
  stopping: '停止中',
  stopped: '已停止',
  resume_failed: '恢复失败',
  failed: '失败',
  blocked: '阻塞',
  completed: '已完成',
  closed: '已关闭',
};

const colors: Record<SessionStatus, string> = {
  created: 'blue-grey',
  queued: 'warning',
  starting: 'primary',
  running: 'positive',
  waiting_user: 'warning',
  waiting_approval: 'warning',
  stopping: 'warning',
  stopped: 'blue-grey',
  resume_failed: 'negative',
  failed: 'negative',
  blocked: 'negative',
  completed: 'primary',
  closed: 'grey',
};

export function sessionStatusLabel(status: SessionStatus) {
  return labels[status];
}

export function sessionStatusColor(status: SessionStatus) {
  return colors[status];
}
