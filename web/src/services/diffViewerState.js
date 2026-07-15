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

export function initialDiffCollapseState(targetKey) {
  return { targetKey, collapsedPaths: [] };
}

export function syncDiffCollapseTarget(state, targetKey) {
  return state.targetKey === targetKey ? state : initialDiffCollapseState(targetKey);
}

export function isDiffFileCollapsed(state, mode, filePath) {
  return mode === 'all' && state.collapsedPaths.includes(filePath);
}

export function toggleDiffFileCollapsed(state, mode, filePath) {
  if (mode !== 'all') return state;
  if (state.collapsedPaths.includes(filePath)) {
    return {
      ...state,
      collapsedPaths: state.collapsedPaths.filter((path) => path !== filePath),
    };
  }
  return { ...state, collapsedPaths: [...state.collapsedPaths, filePath] };
}

export function collapseDiffFiles(state, mode, filePaths) {
  if (mode !== 'all') return state;
  return {
    ...state,
    collapsedPaths: [...new Set([...state.collapsedPaths, ...filePaths])],
  };
}

export function expandDiffFiles(state, mode, filePaths) {
  if (mode !== 'all') return state;
  const expandedPaths = new Set(filePaths);
  return {
    ...state,
    collapsedPaths: state.collapsedPaths.filter((path) => !expandedPaths.has(path)),
  };
}
