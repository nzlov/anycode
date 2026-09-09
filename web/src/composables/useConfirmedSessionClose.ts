import { Dialog } from 'quasar';

import { AnyCodeGraphQLError } from '@/services/graphqlClient';
import {
  closeSession,
  type CloseSessionReason,
} from '@/services/sessions';

const worktreeCloseConfirmationRequired = 'worktree_close_confirmation_required';

export async function closeSessionWithConfirmation(
  sessionId: string,
  reason: CloseSessionReason = 'user_closed',
): Promise<boolean> {
  try {
    await closeSession(sessionId, reason);
    return true;
  } catch (err) {
    if (!(err instanceof AnyCodeGraphQLError) || err.code !== worktreeCloseConfirmationRequired) {
      throw err;
    }
  }

  if (!(await confirmWorktreeClose())) return false;
  await closeSession(sessionId, reason, true);
  return true;
}

function confirmWorktreeClose(): Promise<boolean> {
  return new Promise((resolve) => {
    Dialog.create({
      title: '工作树有未提交文件',
      message: '关闭后会清理工作树，所有未提交的修改都将丢失。仍要关闭卡片吗？',
      cancel: { label: '取消', flat: true },
      persistent: true,
      ok: { label: '仍然关闭', color: 'negative' },
    })
      .onOk(() => resolve(true))
      .onCancel(() => resolve(false))
      .onDismiss(() => resolve(false));
  });
}
