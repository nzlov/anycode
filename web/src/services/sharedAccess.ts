import { applicationStorageKey } from '@/services/applicationPath.js';

export function currentShareId() {
  if (typeof window === 'undefined') return '';
  return /^#\/share\/([^/?#]+)(?:\?|$)/.exec(window.location.hash)?.[1] ?? '';
}
export function shareCredential(id = currentShareId()) {
  return id
    ? (window.localStorage.getItem(applicationStorageKey(`anycode.share.${id}`)) ?? '')
    : '';
}
export function saveShareCredential(id: string, credential: string) {
  window.localStorage.setItem(applicationStorageKey(`anycode.share.${id}`), credential);
}
export function sharedResourcePath(path: string) {
  const id = currentShareId();
  return id ? `/api/shared/${id}${path}` : path;
}
