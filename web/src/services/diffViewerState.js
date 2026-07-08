export const DEFAULT_DIFF_CONTEXT = 10;
export const DIFF_EXPAND_STEP = 20;

export function initialDiffContext() {
  return { before: DEFAULT_DIFF_CONTEXT, after: DEFAULT_DIFF_CONTEXT };
}

export function expandDiffContext(context, direction) {
  if (direction === 'before') {
    return { before: context.before + DIFF_EXPAND_STEP, after: context.after };
  }
  return { before: context.before, after: context.after + DIFF_EXPAND_STEP };
}
