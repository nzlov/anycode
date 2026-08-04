import { browserPreviewKind } from './browserPreviewModel.js';

export function diffMediaKind(filePath) {
  return browserPreviewKind(String(filePath).split('/').pop() ?? '');
}

export function diffMediaVersions(status) {
  if (status === 'added') return ['new'];
  if (status === 'deleted') return ['old'];
  return ['old', 'new'];
}
