import { getGraphQLAccessKey, graphqlFetch } from '@/services/graphqlClient';

export type SessionFilePreviewKind =
  'image' | 'pdf' | 'video' | 'audio' | 'model' | 'text' | 'none';

export interface SessionFile {
  id: string;
  sessionId: string;
  role: string;
  sourceType: string;
  artifactKind: string;
  logicalPath: string;
  filename: string;
  mimeType: string;
  size: number;
  previewKind: SessionFilePreviewKind;
  previewUrl: string | null;
  downloadUrl: string;
  createdAt: string;
  previewRequiresBearer?: boolean;
}

export type SessionFileAccess = Pick<SessionFile, 'filename' | 'previewUrl' | 'downloadUrl'>;
export type SessionFilePreviewData = Pick<
  SessionFile,
  | 'id'
  | 'filename'
  | 'size'
  | 'previewKind'
  | 'previewUrl'
  | 'downloadUrl'
  | 'previewRequiresBearer'
>;

export interface ListSessionFilesInput {
  sessionId: string;
  kind?: string;
  source?: string;
  filter?: string;
  sort?: string;
}

export interface ResolvedSessionArtifact {
  logicalPath: string;
  file: SessionFile;
}

export interface SessionArtifactFocusRequest {
  file: SessionFile;
  token: number;
}

const sessionFileFields = `
  id sessionId role sourceType artifactKind logicalPath filename mimeType size
  previewKind previewUrl downloadUrl createdAt
`;

export async function listSessionFiles(input: ListSessionFilesInput): Promise<SessionFile[]> {
  const data = await graphqlFetch<
    { sessionFiles: SessionFile[] },
    { input: ListSessionFilesInput }
  >({
    query: `
      query SessionFiles($input: ListSessionFilesInput!) {
        sessionFiles(input: $input) { ${sessionFileFields} }
      }
    `,
    variables: { input },
  });
  return data.sessionFiles;
}

export async function resolveSessionArtifacts(
  sessionId: string,
  logicalPaths: string[],
): Promise<ResolvedSessionArtifact[]> {
  const data = await graphqlFetch<
    { resolveSessionArtifacts: ResolvedSessionArtifact[] },
    { input: { sessionId: string; logicalPaths: string[] } }
  >({
    query: `
      query ResolveSessionArtifacts($input: ResolveSessionArtifactsInput!) {
        resolveSessionArtifacts(input: $input) {
          logicalPath
          file { ${sessionFileFields} }
        }
      }
    `,
    variables: { input: { sessionId, logicalPaths } },
  });
  return data.resolveSessionArtifacts;
}

export async function resolveSessionWorkspaceFile(
  sessionId: string,
  path: string,
  signal?: AbortSignal,
): Promise<SessionFile> {
  const query = new URLSearchParams({ path });
  const previewUrl = `/api/sessions/${encodeURIComponent(sessionId)}/workspace-file?${query}`;
  const response = await fetch(previewUrl, {
    method: 'HEAD',
    headers: sessionFileHeaders(),
    signal: signal ?? null,
  });
  if (!response.ok) throw new Error(`读取工作区文件失败：HTTP ${response.status}`);
  const previewKind = response.headers.get(
    'x-anycode-preview-kind',
  ) as SessionFilePreviewKind | null;
  if (
    !previewKind ||
    !['image', 'pdf', 'video', 'audio', 'model', 'text', 'none'].includes(previewKind)
  ) {
    throw new Error('工作区文件预览响应无效');
  }
  const filename = path.replaceAll('\\', '/').split('/').pop() || 'workspace-file';
  const size = Number(response.headers.get('content-length') || 0);
  const downloadQuery = new URLSearchParams({ path, download: '1' });
  // GLUE: Workspace files use the existing session-file preview contract; remove when the viewer accepts workspace sources directly.
  return {
    id: `workspace:${sessionId}:${path}`,
    sessionId,
    role: 'workspace',
    sourceType: 'workspace',
    artifactKind: previewKind === 'none' ? 'file' : previewKind,
    logicalPath: path,
    filename,
    mimeType: response.headers.get('content-type') || 'application/octet-stream',
    size: Number.isFinite(size) ? size : 0,
    previewKind,
    previewUrl,
    downloadUrl: `/api/sessions/${encodeURIComponent(sessionId)}/workspace-file?${downloadQuery}`,
    createdAt: '',
    previewRequiresBearer: true,
  };
}

export async function deleteSessionFile(id: string): Promise<boolean> {
  const data = await graphqlFetch<{ deleteSessionFile: boolean }, { id: string }>({
    query: `mutation DeleteSessionFile($id: ID!) { deleteSessionFile(id: $id) }`,
    variables: { id },
  });
  return data.deleteSessionFile;
}

export async function fetchSessionFile(
  file: SessionFileAccess,
  mode: 'preview' | 'download',
  signal?: AbortSignal,
) {
  const url = mode === 'preview' ? file.previewUrl : file.downloadUrl;
  if (!url) throw new Error('当前文件不支持预览');
  const headers = sessionFileHeaders();
  const response = await fetch(url, { headers, signal: signal ?? null });
  if (!response.ok) throw new Error(`读取文件失败：HTTP ${response.status}`);
  return response.blob();
}

export async function requestSessionFilePreviewURL(
  file: SessionFilePreviewData,
  signal?: AbortSignal,
) {
  const response = await fetch(`/files/${encodeURIComponent(file.id)}/preview-token`, {
    method: 'POST',
    headers: sessionFileHeaders(),
    signal: signal ?? null,
  });
  if (!response.ok) throw new Error(`获取文件预览凭据失败：HTTP ${response.status}`);
  const payload = (await response.json()) as { url?: unknown };
  if (typeof payload.url !== 'string' || !payload.url.startsWith('/files/')) {
    throw new Error('文件预览凭据响应无效');
  }
  return payload.url;
}

function sessionFileHeaders() {
  const headers = new Headers();
  const accessKey = getGraphQLAccessKey();
  if (accessKey) headers.set('authorization', `Bearer ${accessKey}`);
  return headers;
}

export async function downloadSessionFile(file: SessionFileAccess) {
  const blob = await fetchSessionFile(file, 'download');
  const url = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement('a');
    anchor.href = url;
    anchor.download = file.filename;
    anchor.click();
  } finally {
    URL.revokeObjectURL(url);
  }
}
