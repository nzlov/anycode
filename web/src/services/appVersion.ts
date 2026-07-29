import { getGraphQLAccessKey } from '@/services/graphqlClient';
import { hasNewerRelease } from '@/services/appVersionModel.js';

const latestReleaseURL = 'https://api.github.com/repos/nzlov/anycode/releases/latest';

export interface AppRelease {
  version: string;
  name: string;
  body: string;
  url: string;
  publishedAt: string;
}

export interface AppVersionStatus {
  currentVersion: string;
  availableRelease: AppRelease | null;
}

interface BuildVersionResponse {
  version: string;
}

interface GitHubReleaseResponse {
  tag_name?: unknown;
  name?: unknown;
  body?: unknown;
  published_at?: unknown;
}

export async function getAppVersionStatus(): Promise<AppVersionStatus> {
  const currentVersion = await getBuildVersion();
  const release = await getLatestRelease().catch(() => null);
  return {
    currentVersion,
    availableRelease: release && hasNewerRelease(currentVersion, release.version) ? release : null,
  };
}

async function getBuildVersion() {
  const headers = new Headers();
  const accessKey = getGraphQLAccessKey();
  if (accessKey) headers.set('authorization', `Bearer ${accessKey}`);
  const response = await fetch('/api/version', { headers });
  if (!response.ok) throw new Error(`读取程序版本失败：HTTP ${response.status}`);
  const payload = (await response.json()) as BuildVersionResponse;
  return typeof payload.version === 'string' && payload.version.trim()
    ? payload.version.trim()
    : 'dev';
}

async function getLatestRelease(): Promise<AppRelease | null> {
  const response = await fetch(latestReleaseURL, {
    headers: { Accept: 'application/vnd.github+json' },
  });
  if (response.status === 404) return null;
  if (!response.ok) throw new Error(`读取最新版本失败：HTTP ${response.status}`);
  const payload = (await response.json()) as GitHubReleaseResponse;
  if (typeof payload.tag_name !== 'string') return null;
  return {
    version: payload.tag_name,
    name: typeof payload.name === 'string' && payload.name ? payload.name : payload.tag_name,
    body:
      typeof payload.body === 'string' && payload.body ? payload.body : '此版本未提供更新说明。',
    url: `https://github.com/nzlov/anycode/releases/tag/${encodeURIComponent(payload.tag_name)}`,
    publishedAt: typeof payload.published_at === 'string' ? payload.published_at : '',
  };
}
