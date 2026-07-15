export function normalizeArtifactLogicalPath(value: string): string | null {
  const normalizedSlashes = value.trim().replaceAll('\\', '/');
  if (
    !normalizedSlashes ||
    normalizedSlashes.startsWith('/') ||
    /^[A-Za-z]:/.test(normalizedSlashes)
  ) {
    return null;
  }
  const segments = normalizedSlashes.split('/');
  if (segments.some((segment) => segment === '.' || segment === '..')) return null;
  const normalized = segments.filter(Boolean).join('/');
  return normalized || null;
}
