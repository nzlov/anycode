const thumbnailThreshold = 10 * 1024 * 1024;
const thumbnailMaxEdge = 2048;

// Shared by attachment/workspace and Diff previews; callers own the returned blob URL.
export async function loadImagePreview(url: string, signal: AbortSignal, size?: number) {
  signal.throwIfAborted();
  if (size !== undefined && size <= thumbnailThreshold) return { url, thumbnail: false };

  const response = await fetch(url, { signal });
  if (!response.ok) throw new Error(`读取图片失败：HTTP ${response.status}`);
  const blob = await response.blob();
  signal.throwIfAborted();
  if (blob.size <= thumbnailThreshold) {
    return { url: URL.createObjectURL(blob), thumbnail: false };
  }

  const bitmap = await createImageBitmap(blob);
  try {
    signal.throwIfAborted();
    const scale = Math.min(1, thumbnailMaxEdge / Math.max(bitmap.width, bitmap.height));
    const canvas = document.createElement('canvas');
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    try {
      const context = canvas.getContext('2d');
      if (!context) throw new Error('无法生成缩略图，请下载原图');
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      const thumbnail = await new Promise<Blob>((resolve, reject) => {
        canvas.toBlob(
          (result) => (result ? resolve(result) : reject(new Error('生成缩略图失败，请下载原图'))),
          'image/webp',
          0.85,
        );
      });
      signal.throwIfAborted();
      return { url: URL.createObjectURL(thumbnail), thumbnail: true };
    } finally {
      canvas.width = canvas.height = 0;
    }
  } finally {
    bitmap.close();
  }
}
