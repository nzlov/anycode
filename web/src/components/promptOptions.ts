export interface PromptOption {
  label: string;
  value: string;
}

export interface CodexModelOption extends PromptOption {
  reasoningEfforts: PromptOption[];
}

const reasoningEffortLabels: Record<string, string> = {
  low: '低思考',
  medium: '中等思考',
  high: '高思考',
  xhigh: '极高思考',
};

function modelOption(label: string, value: string, efforts: string[]): CodexModelOption {
  return {
    label,
    value,
    reasoningEfforts: efforts.map((effort) => ({
      label: reasoningEffortLabels[effort] ?? effort,
      value: effort,
    })),
  };
}

export const codexModelOptions: CodexModelOption[] = [
  modelOption('GPT-5.5', 'gpt-5.5', ['low', 'medium', 'high', 'xhigh']),
  modelOption('GPT-5.4', 'gpt-5.4', ['low', 'medium', 'high', 'xhigh']),
  modelOption('GPT-5.4-Mini', 'gpt-5.4-mini', ['low', 'medium', 'high', 'xhigh']),
  modelOption('GPT-5.3 Codex', 'gpt-5.3-codex', ['low', 'medium', 'high', 'xhigh']),
  modelOption('GPT-5.2', 'gpt-5.2', ['low', 'medium', 'high', 'xhigh']),
];

export function firstCodexModelValue() {
  return codexModelOptions[0]?.value ?? '';
}

export function normalizeCodexModel(value: string) {
  if (codexModelOptions.some((option) => option.value === value)) return value;
  return firstCodexModelValue();
}

export function codexModelLabel(value: string) {
  return codexModelOptions.find((option) => option.value === value)?.label ?? (value || '-');
}

export function reasoningEffortOptionsForModel(model: string) {
  return (
    codexModelOptions.find((option) => option.value === model)?.reasoningEfforts ??
    codexModelOptions[0]?.reasoningEfforts ??
    []
  );
}

export function normalizeReasoningEffort(model: string, value: string) {
  const options = reasoningEffortOptionsForModel(model);
  if (options.some((option) => option.value === value)) return value;
  return options[0]?.value ?? '';
}

export function defaultReasoningEffortForModel(model: string) {
  return reasoningEffortOptionsForModel(model)[0]?.value ?? '';
}

export type PromptConfigUpdate =
  { field: 'model'; value: string } | { field: 'effort'; value: string };

export function promptConfigUpdatesForModelChange(
  value: string,
  currentEffort: string,
): PromptConfigUpdate[] {
  const model = normalizeCodexModel(value);
  const effort = defaultReasoningEffortForModel(model);
  const updates: PromptConfigUpdate[] = [{ field: 'model', value: model }];
  if (effort !== currentEffort) {
    updates.push({ field: 'effort', value: effort });
  }
  return updates;
}

export function reasoningEffortLabel(model: string, value: string) {
  return (
    reasoningEffortOptionsForModel(model).find((option) => option.value === value)?.label ??
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
