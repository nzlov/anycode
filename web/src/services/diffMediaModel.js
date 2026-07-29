import { modelFileFormat } from './modelFiles.js';

const imageExtensions = new Set(['gif', 'jpeg', 'jpg', 'png', 'webp']);
const audioExtensions = new Set(['aac', 'flac', 'm4a', 'mp3', 'oga', 'ogg', 'wav']);
const videoExtensions = new Set(['m4v', 'mov', 'mp4', 'ogv', 'webm']);

export function diffMediaKind(filePath) {
  const filename = String(filePath).split('/').pop() ?? '';
  const extension = filename.includes('.') ? filename.split('.').pop().toLowerCase() : '';
  if (imageExtensions.has(extension)) return 'image';
  if (audioExtensions.has(extension)) return 'audio';
  if (videoExtensions.has(extension)) return 'video';
  if (modelFileFormat(filename)) return 'model';
  return null;
}

export function diffMediaVersions(status) {
  if (status === 'added') return ['new'];
  if (status === 'deleted') return ['old'];
  return ['old', 'new'];
}
