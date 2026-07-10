import type { FileDiff } from './diff';

export function fileDiffFromUnifiedDiff(
  path: string,
  status: string,
  unifiedDiff: string,
): FileDiff | null;
