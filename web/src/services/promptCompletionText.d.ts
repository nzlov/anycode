export type PromptCompletionRange = {
  kind: 'command' | 'file';
  query: string;
  start: number;
  end: number;
};

export type PromptSlashCommand = {
  name: string;
  description: string;
  acceptsArgs: boolean;
  requiresThread: boolean;
};

export function activePromptCompletion(text: string, cursor?: number): PromptCompletionRange | null;
export function filterSlashCommands(
  commands: PromptSlashCommand[],
  query: string,
  hasThread: boolean,
): PromptSlashCommand[];
export function applyPromptCompletion(
  text: string,
  range: Pick<PromptCompletionRange, 'start' | 'end'>,
  value: string,
): string;
export function formatFileMention(path: string): string;
export function promptMatchSegments(
  text: string,
  indices: number[],
): Array<{ text: string; matched: boolean }>;
