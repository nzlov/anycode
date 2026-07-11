import type { QuickCommand } from './quickCommands';

export function shouldApplyQuickCommandSnapshot(
  requestId: number,
  latestRequestId: number,
  loadMutationVersion: number,
  currentMutationVersion: number,
): boolean;
export function prependQuickCommand(
  items: QuickCommand[],
  command: QuickCommand,
  pageSize: number,
): QuickCommand[];
export function removeQuickCommandById(items: QuickCommand[], id: string): QuickCommand[];
