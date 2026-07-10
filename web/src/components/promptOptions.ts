export interface PromptOption {
  label: string;
  value: string;
  description?: string;
}

export interface CodexModelOption extends PromptOption {
  defaultReasoningEffort: string;
  reasoningEfforts: PromptOption[];
}

const reasoningEffortLabels: Record<string, string> = {
  low: '低思考',
  medium: '中等思考',
  high: '高思考',
  xhigh: '极高思考',
  max: '最强思考',
  ultra: '极限思考',
};

export function normalizeCodexModelOptions(options: CodexModelOption[]) {
  return options.map((option) => ({
    ...option,
    reasoningEfforts: option.reasoningEfforts.map((effort) => ({
      ...effort,
      label: reasoningEffortLabels[effort.value] ?? effort.label,
    })),
  }));
}

export function firstCodexModelValue(options: CodexModelOption[]) {
  return options[0]?.value ?? '';
}

export function normalizeCodexModel(options: CodexModelOption[], value: string) {
  if (options.length === 0) return value;
  if (options.some((option) => option.value === value)) return value;
  return firstCodexModelValue(options);
}

export function codexModelLabel(options: CodexModelOption[], value: string) {
  return options.find((option) => option.value === value)?.label ?? (value || '-');
}

export function reasoningEffortOptionsForModel(options: CodexModelOption[], model: string) {
  return (
    options.find((option) => option.value === model)?.reasoningEfforts ??
    options[0]?.reasoningEfforts ??
    []
  );
}

export function normalizeReasoningEffort(options: CodexModelOption[], model: string, value: string) {
  if (options.length === 0) return value;
  const efforts = reasoningEffortOptionsForModel(options, model);
  if (efforts.some((option) => option.value === value)) return value;
  return defaultReasoningEffortForModel(options, model);
}

export function defaultReasoningEffortForModel(options: CodexModelOption[], model: string) {
  const modelOption = options.find((option) => option.value === model) ?? options[0];
  if (!modelOption) return '';
  if (
    modelOption.defaultReasoningEffort &&
    modelOption.reasoningEfforts.some((option) => option.value === modelOption.defaultReasoningEffort)
  ) {
    return modelOption.defaultReasoningEffort;
  }
  return modelOption.reasoningEfforts[0]?.value ?? '';
}

export type PromptConfigUpdate =
  { field: 'model'; value: string } | { field: 'effort'; value: string };

export function promptConfigUpdatesForModelChange(
  options: CodexModelOption[],
  value: string,
  currentEffort: string,
): PromptConfigUpdate[] {
  const model = normalizeCodexModel(options, value);
  const effort = defaultReasoningEffortForModel(options, model);
  const updates: PromptConfigUpdate[] = [{ field: 'model', value: model }];
  if (effort !== currentEffort) {
    updates.push({ field: 'effort', value: effort });
  }
  return updates;
}

export function reasoningEffortLabel(options: CodexModelOption[], model: string, value: string) {
  return (
    reasoningEffortOptionsForModel(options, model).find((option) => option.value === value)?.label ??
    (value || '-')
  );
}

export const permissionModeOptions = [
  { label: '只读', value: 'read-only', icon: 'visibility' },
  { label: '工作区写入', value: 'workspace-write', icon: 'edit_note' },
  { label: '完全访问', value: 'danger-full-access', icon: 'warning' },
];

export function normalizePermissionMode(value: string) {
  if (permissionModeOptions.some((option) => option.value === value)) return value;
  return permissionModeOptions[0]?.value ?? '';
}

export function permissionModeLabel(value: string) {
  return permissionModeOptions.find((option) => option.value === value)?.label ?? (value || '-');
}
