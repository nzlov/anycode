export interface PromptOption {
  label: string;
  value: string;
  description?: string;
}

export interface CodexModelOption extends PromptOption {
  defaultReasoningEffort: string;
  reasoningEfforts: PromptOption[];
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

export function normalizeCodexSelection(
  options: CodexModelOption[],
  model: string,
  effort: string,
) {
  const normalizedModel = normalizeCodexModel(options, model);
  return {
    model: normalizedModel,
    effort: normalizeReasoningEffort(options, normalizedModel, effort),
  };
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
