export function shouldApplyQuickCommandSnapshot(
  requestId,
  latestRequestId,
  loadMutationVersion,
  currentMutationVersion,
) {
  return requestId === latestRequestId && loadMutationVersion === currentMutationVersion;
}

export function prependQuickCommand(items, command, pageSize) {
  if (items.some((item) => item.id === command.id)) return items;
  return [command, ...items].slice(0, pageSize);
}

export function removeQuickCommandById(items, id) {
  return items.filter((item) => item.id !== id);
}
