export type BrowserPreviewKind = 'image' | 'audio' | 'video' | 'pdf' | 'model';

export function browserPreviewKind(filename: string, mimeType?: string): BrowserPreviewKind | null;
