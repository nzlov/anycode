export function eventAfterId(events) {
  return events.at(-1)?.id ?? '';
}

export function appendLiveEvent(events, event) {
  if (events.some((item) => item.id === event.id)) return events;
  return [...events, event];
}

export function prependOlderEvents(events, olderEvents) {
  const existing = new Set(events.map((event) => event.id));
  const older = olderEvents.filter((event) => !existing.has(event.id));
  if (older.length === 0) return events;
  return [...older, ...events];
}

export function shouldRefreshSessionForEvent(event, liveOnly, replayStateCanRefresh = false) {
  if (!liveOnly && !replayStateCanRefresh) return false;
  const type = event?.rawType ?? '';
  return type.startsWith('session.') || type.startsWith('workflow.');
}

export function isEventAtOrAfter(event, timestamp) {
  const createdAt = Date.parse(event?.createdAt ?? '');
  return Number.isFinite(createdAt) && createdAt >= timestamp;
}
