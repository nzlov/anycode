import { getGraphQLAccessKey, graphqlFetch } from '@/services/graphqlClient';

export type SessionFilePreviewKind = 'image' | 'pdf' | 'video' | 'audio' | 'text' | 'none';

export interface SessionFile {
  id: string;
  sessionId: string;
  role: string;
  sourceType: string;
  sourceId: string;
  artifactKind: string;
  logicalPath: string;
  filename: string;
  mimeType: string;
  size: number;
  sha256: string;
  previewKind: SessionFilePreviewKind;
  processRunId: string | null;
  nodeRunId: string | null;
  correlationId: string;
  previewUrl: string | null;
  downloadUrl: string;
  createdAt: string;
}

export type SessionFileAccess = Pick<SessionFile, 'filename' | 'previewUrl' | 'downloadUrl'>;

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
  id sessionId role sourceType sourceId artifactKind logicalPath filename mimeType size sha256
  previewKind processRunId nodeRunId correlationId previewUrl downloadUrl createdAt
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

export async function deleteSessionFile(id: string): Promise<boolean> {
  const data = await graphqlFetch<{ deleteSessionFile: boolean }, { id: string }>({
    query: `mutation DeleteSessionFile($id: ID!) { deleteSessionFile(id: $id) }`,
    variables: { id },
  });
  return data.deleteSessionFile;
}

export async function useSessionFileAsInput(id: string): Promise<{ id: string; filename: string }> {
  const data = await graphqlFetch<
    { useSessionFileAsInput: { id: string; filename: string } },
    { id: string }
  >({
    query: `mutation UseSessionFileAsInput($id: ID!) { useSessionFileAsInput(id: $id) { id filename } }`,
    variables: { id },
  });
  return data.useSessionFileAsInput;
}

export async function fetchSessionFile(
  file: SessionFileAccess,
  mode: 'preview' | 'download',
  signal?: AbortSignal,
) {
  const url = mode === 'preview' ? file.previewUrl : file.downloadUrl;
  if (!url) throw new Error('当前文件不支持预览');
  const headers = new Headers();
  const accessKey = getGraphQLAccessKey();
  if (accessKey) headers.set('authorization', `Bearer ${accessKey}`);
  const response = await fetch(url, { headers, signal: signal ?? null });
  if (!response.ok) throw new Error(`读取文件失败：HTTP ${response.status}`);
  return response.blob();
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
