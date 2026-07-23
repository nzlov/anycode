import type { SessionMode } from '@/services/sessions';

export function sessionModeBadgeLabel(mode: SessionMode) {
  return mode === 'workflow' ? '流程' : '对话';
}
