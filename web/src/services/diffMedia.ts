import { getGraphQLAccessKey } from '@/services/graphqlClient';

import type { DiffMediaVersion } from '@/services/diffMediaModel';

export async function requestDiffMediaPreviewURL(
  sessionId: string,
  filePath: string,
  version: DiffMediaVersion,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ path: filePath, version });
  const headers = new Headers();
  const accessKey = getGraphQLAccessKey();
  if (accessKey) headers.set('authorization', `Bearer ${accessKey}`);
  const response = await fetch(
    `/api/sessions/${encodeURIComponent(sessionId)}/diff-media/preview-token?${query.toString()}`,
    { method: 'POST', headers, signal: signal ?? null },
  );
  if (!response.ok)
    throw new Error(`读取${version === 'old' ? '旧' : '新'}版本失败：HTTP ${response.status}`);
  const payload = (await response.json()) as { url?: unknown };
  if (typeof payload.url !== 'string') throw new Error('文件预览凭据响应无效');
  const target = new URL(payload.url, window.location.origin);
  if (
    target.origin !== window.location.origin ||
    target.pathname !== `/api/sessions/${encodeURIComponent(sessionId)}/diff-media`
  ) {
    throw new Error('文件预览地址无效');
  }
  return payload.url;
}
