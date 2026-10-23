// Local previews share the host origin, but have separate routes and credentials.
export function applicationBasePath() {
  if (typeof window === 'undefined') return '';
  return window.location.pathname?.match(/^\/tunnel\/[A-Za-z0-9_-]+(?=\/|$)/)?.[0] || '';
}

export function applicationURL(path) {
  const base = applicationBasePath();
  if (
    !base ||
    !path.startsWith('/') ||
    path.startsWith('//') ||
    path === base ||
    path.startsWith(`${base}/`)
  )
    return path;
  return `${base}${path}`;
}

export function applicationStorageKey(key) {
  const base = applicationBasePath();
  return base ? `${key}:${base}` : key;
}

export function applicationAuthorizationHeader() {
  return applicationBasePath() ? 'x-anycode-instance-authorization' : 'authorization';
}
