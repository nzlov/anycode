import {
  applicationURL,
  applicationAuthorizationHeader,
  applicationStorageKey,
} from '@/services/applicationPath.js';
import { getGraphQLAccessKey } from '@/services/graphqlClient';
import { shareCredential } from '@/services/sharedAccess';
import type { SessionConfig } from '@/services/sessions';

export interface SessionShare {
  id: string;
  sessionId: string;
  createdAt: string;
  claimedAt: string | null;
  revokedAt: string | null;
}
export interface ShareMessage {
  id: string;
  shareId: string;
  sessionId: string;
  status: string;
  createdAt: string;
  content: {
    body: string;
    config: {
      CodexModel: string;
      ReasoningEffort: string;
      PermissionMode: string;
      FastMode: boolean;
    };
    attachments: { id: string; filename: string; mimeType: string; size: number }[];
  };
}
export class SharingRequestError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}
export async function sharingRequest<T>(
  path: string,
  options: RequestInit = {},
  credential = getGraphQLAccessKey(),
): Promise<T> {
  const headers = new Headers(options.headers);
  if (credential) headers.set(applicationAuthorizationHeader(), `Bearer ${credential}`);
  const response = await fetch(applicationURL(path), { ...options, headers });
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as {
      message?: string;
      error?: { message?: string };
    } | null;
    throw new SharingRequestError(
      payload?.error?.message ?? payload?.message ?? `请求失败：${response.status}`,
      response.status,
    );
  }
  return response.status === 204 ? (undefined as T) : (response.json() as Promise<T>);
}
export const listSessionShares = (id: string) =>
  sharingRequest<SessionShare[]>(`/api/sessions/${id}/shares`);
export const listShareMessages = (id: string) =>
  sharingRequest<ShareMessage[]>(`/api/sessions/${id}/share-messages`);
export const revokeSessionShare = (id: string) =>
  sharingRequest<void>(`/api/session-shares/${id}`, { method: 'DELETE' });
export const reviewShareMessage = (id: string, approved: boolean) =>
  sharingRequest<ShareMessage>(`/api/share-messages/${id}/review`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ approved }),
  });
export async function createSessionShare(sessionId: string) {
  const result = await sharingRequest<{ share: SessionShare; token: string }>(
    `/api/sessions/${sessionId}/shares`,
    { method: 'POST' },
  );
  const url = new URL(window.location.href);
  url.hash = `/share/${result.share.id}?token=${result.token}`;
  window.localStorage.setItem(
    applicationStorageKey(`anycode.share-link.${result.share.id}`),
    url.href,
  );
  return { ...result.share, url: url.href };
}
export const savedShareURL = (id: string) =>
  window.localStorage.getItem(applicationStorageKey(`anycode.share-link.${id}`)) ?? '';
export const claimSessionShare = (id: string, token: string) =>
  sharingRequest<{ share: SessionShare; credential: string }>(
    `/api/shared/${id}/claim`,
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
    },
    '',
  );
export const getSharedSession = (id: string) =>
  sharingRequest<SessionShare>(`/api/shared/${id}`, {}, shareCredential(id));
export const getGuestMessages = (id: string) =>
  sharingRequest<ShareMessage[]>(`/api/shared/${id}/messages`, {}, shareCredential(id));
export function submitSharedMessage(
  id: string,
  body: string,
  config: SessionConfig,
  files: File[],
) {
  const form = new FormData();
  form.set('body', body);
  form.set(
    'config',
    JSON.stringify({
      CodexModel: config.codexModel,
      ReasoningEffort: config.reasoningEffort,
      PermissionMode: config.permissionMode,
      FastMode: config.fastMode,
    }),
  );
  for (const file of files) form.append('files', file);
  return sharingRequest<ShareMessage>(
    `/api/shared/${id}/messages`,
    { method: 'POST', body: form },
    shareCredential(id),
  );
}
export async function downloadReviewAttachment(
  messageId: string,
  file: { id: string; filename: string },
) {
  const response = await fetch(
    applicationURL(`/api/share-messages/${messageId}/files/${file.id}`),
    { headers: { [applicationAuthorizationHeader()]: `Bearer ${getGraphQLAccessKey()}` } },
  );
  if (!response.ok) throw new Error('读取审核附件失败');
  const url = URL.createObjectURL(await response.blob());
  const a = document.createElement('a');
  a.href = url;
  a.download = file.filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}
export function shareMessageStatus(status: string) {
  return (
    (
      {
        pending: '待审核',
        approved: '已批准，排队中',
        sending: '正在发送 / 待确认',
        sent: '已发送',
        rejected: '已拒绝',
        cancelled: '分享已撤销',
        failed: '发送失败，请检查会话后重新提交',
      } as Record<string, string>
    )[status] ?? status
  );
}
