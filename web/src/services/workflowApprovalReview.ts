import type { PendingApproval, WorkflowNodeResult } from './sessions';

const outcomes = new Set(['success', 'partial', 'failure']);
const checkStatuses = new Set(['passed', 'warning', 'failed']);
const checkSources = new Set(['agent', 'system']);

export interface WorkflowResultDataProjection {
  entries: Array<{ key: string; value: unknown }>;
  truncated: boolean;
}

export function flattenWorkflowResultData(
  data: Record<string, unknown>,
  maxDepth = 8,
  maxEntries = 100,
): WorkflowResultDataProjection {
  const entries: WorkflowResultDataProjection['entries'] = [];
  const stack = Object.entries(data).reverse().map(([key, value]) => ({ key, value, depth: 1 }));
  let truncated = false;

  while (stack.length > 0 && entries.length < maxEntries) {
    const current = stack.pop();
    if (!current) break;
    if (!isRecord(current.value)) {
      entries.push({ key: current.key, value: current.value });
      continue;
    }
    if (current.depth >= maxDepth) {
      entries.push({ key: current.key, value: '[内容层级过深，请查看原始结果]' });
      truncated = true;
      continue;
    }
    const children = Object.entries(current.value);
    if (children.length === 0) {
      entries.push({ key: current.key, value: '{}' });
      continue;
    }
    for (let index = children.length - 1; index >= 0; index -= 1) {
      const child = children[index];
      if (!child) continue;
      const [key, value] = child;
      stack.push({ key: `${current.key}.${key}`, value, depth: current.depth + 1 });
    }
  }
  if (stack.length > 0) truncated = true;
  return { entries, truncated };
}

export function normalizeWorkflowNodeResult(value: unknown): WorkflowNodeResult | null {
  if (!isRecord(value)) return null;
  if (
    value.version !== 1 ||
    !outcomes.has(String(value.outcome)) ||
    typeof value.summary !== 'string' ||
    value.summary.trim() === '' ||
    !isRecord(value.data) ||
    !Array.isArray(value.checks) ||
    !value.checks.every(isResultCheck) ||
    !Array.isArray(value.warnings) ||
    !value.warnings.every(isResultWarning) ||
    !Array.isArray(value.artifacts) ||
    !value.artifacts.every(isResultArtifact)
  ) {
    return null;
  }
  return value as unknown as WorkflowNodeResult;
}

export function isPendingApprovalReviewable(
  approval?: PendingApproval | null,
): approval is PendingApproval {
  if (!approval) return false;
  if (approval.phase === 'before_run') return true;
  return approval.phase === 'after_run' && normalizeWorkflowNodeResult(approval.result) !== null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function isResultCheck(value: unknown): boolean {
  return isRecord(value) &&
    hasText(value.id) &&
    hasText(value.label) &&
    checkStatuses.has(String(value.status)) &&
    checkSources.has(String(value.source)) &&
    (value.detail === undefined || typeof value.detail === 'string');
}

function isResultWarning(value: unknown): boolean {
  return isRecord(value) && hasText(value.code) && hasText(value.message);
}

function isResultArtifact(value: unknown): boolean {
  return isRecord(value) && hasText(value.kind) && hasText(value.label) && hasText(value.ref);
}

function hasText(value: unknown): value is string {
  return typeof value === 'string' && value.trim() !== '';
}
