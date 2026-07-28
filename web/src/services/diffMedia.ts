import { getGraphQLAccessKey } from '@/services/graphqlClient';

import type { DiffMediaVersion } from '@/services/diffMediaModel';

export async function fetchDiffMedia(
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
    `/api/sessions/${encodeURIComponent(sessionId)}/diff-media?${query.toString()}`,
    { headers, signal: signal ?? null },
  );
  if (!response.ok)
    throw new Error(`读取${version === 'old' ? '旧' : '新'}版本失败：HTTP ${response.status}`);
  return response.blob();
}
