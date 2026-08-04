import { modelFileFormat } from './modelFiles.js';

const imageExtensions = new Set([
  'apng',
  'avif',
  'bmp',
  'gif',
  'ico',
  'jfif',
  'jpeg',
  'jpg',
  'pjp',
  'pjpeg',
  'png',
  'svg',
  'webp',
]);
const audioExtensions = new Set(['aac', 'flac', 'm4a', 'mp3', 'oga', 'ogg', 'opus', 'wav', 'weba']);
const videoExtensions = new Set(['m4v', 'mov', 'mp4', 'mpeg', 'mpg', 'ogv', 'webm']);
const imageMIMETypes = new Set([
  'image/apng',
  'image/avif',
  'image/bmp',
  'image/gif',
  'image/jpeg',
  'image/png',
  'image/svg+xml',
  'image/vnd.microsoft.icon',
  'image/webp',
  'image/x-icon',
]);

export function browserPreviewKind(filename, mimeType = '') {
  if (modelFileFormat(filename)) return 'model';
  const extension = String(filename).split(/[?#]/, 1)[0]?.split('.').pop()?.toLowerCase() ?? '';
  if (imageExtensions.has(extension)) return 'image';
  if (audioExtensions.has(extension)) return 'audio';
  if (videoExtensions.has(extension)) return 'video';
  if (extension === 'pdf') return 'pdf';

  const normalizedMIMEType = String(mimeType).split(';', 1)[0].trim().toLowerCase();
  if (imageMIMETypes.has(normalizedMIMEType)) return 'image';
  if (normalizedMIMEType.startsWith('audio/')) return 'audio';
  if (normalizedMIMEType.startsWith('video/')) return 'video';
  if (normalizedMIMEType === 'application/pdf') return 'pdf';
  return null;
}
