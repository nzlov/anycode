// GLUE: Native Node tests require the extension; remove when they run through the Vite resolver.
// @ts-expect-error TypeScript otherwise rejects source extensions even though Vite and Node support them.
import { normalizeArtifactLogicalPath } from './artifactLogicalPath.ts';

export type SessionEventResourceReference =
  | { kind: 'session-file'; fileId: string }
  | { kind: 'artifact'; logicalPath: string }
  | { kind: 'workspace'; path: string };

export function parseSessionEventResourceReference(
  value: string,
  sessionId: string,
): SessionEventResourceReference | null {
  const reference = decodeReference(value);
  if (!reference || reference.startsWith('#')) return null;

  const fileMatch = stripQueryAndHash(reference).match(/^\/files\/([^/]+)\/(?:preview|download)$/);
  if (fileMatch?.[1]) {
    return { kind: 'session-file', fileId: fileMatch[1] };
  }

  const path = reference.startsWith('file:') ? fileURLPath(reference) : reference;
  if (!path || hasExternalScheme(path)) return null;
  const normalizedPath = stripSourceLocation(stripQueryAndHash(path).replaceAll('\\', '/'));
  const artifactMarker = `/attachments/outputs/${sessionId}/`;
  const artifactIndex = normalizedPath.indexOf(artifactMarker);
  if (artifactIndex >= 0) {
    const logicalPath = normalizeArtifactLogicalPath(
      normalizedPath.slice(artifactIndex + artifactMarker.length),
    );
    return logicalPath ? { kind: 'artifact', logicalPath } : null;
  }
  if (normalizedPath.startsWith('/')) return { kind: 'workspace', path: normalizedPath };
  const logicalPath = normalizeArtifactLogicalPath(normalizedPath);
  return logicalPath ? { kind: 'workspace', path: logicalPath } : null;
}

export function matchChangedFilePath(referencePath: string, changedPaths: readonly string[]) {
  const normalizedReference = referencePath.replaceAll('\\', '/');
  if (!normalizedReference.startsWith('/')) {
    return changedPaths.find((path) => path === normalizedReference) ?? null;
  }
  const matches = changedPaths.filter(
    (path) => normalizedReference === path || normalizedReference.endsWith(`/${path}`),
  );
  return matches.sort((left, right) => right.length - left.length)[0] ?? null;
}

function decodeReference(value: string) {
  const trimmed = value.trim();
  try {
    return decodeURIComponent(trimmed);
  } catch {
    return trimmed;
  }
}

function stripQueryAndHash(value: string) {
  return value.split(/[?#]/, 1)[0] ?? '';
}

function stripSourceLocation(value: string) {
  return value.replace(/:\d+(?::\d+)?$/, '');
}

function fileURLPath(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === 'file:' ? url.pathname : '';
  } catch {
    return '';
  }
}

function hasExternalScheme(value: string) {
  return /^[A-Za-z][A-Za-z\d+.-]*:/.test(value);
}
