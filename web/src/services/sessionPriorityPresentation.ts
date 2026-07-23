import type { SessionPriority } from '@/services/sessions';

export const sessionPriorities: readonly SessionPriority[] = ['high', 'medium', 'low'];

export function sessionPriorityLabel(priority: SessionPriority) {
  const labels: Record<SessionPriority, string> = {
    high: '高优先级',
    medium: '中优先级',
    low: '低优先级',
  };
  return labels[priority];
}
