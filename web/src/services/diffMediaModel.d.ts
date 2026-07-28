export type DiffMediaKind = 'image' | 'audio' | 'video';
export type DiffMediaVersion = 'old' | 'new';

export interface DiffMediaPreviewTarget {
  sessionId: string;
  filePath: string;
  kind: DiffMediaKind;
}

export function diffMediaKind(filePath: string): DiffMediaKind | null;
export function diffMediaVersions(status: string): DiffMediaVersion[];
