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

export function mergeSnapshotEvents(snapshotEvents, currentEvents, bufferedEvents) {
  const merged = new Map(currentEvents.map((event) => [event.id, event]));
  for (const event of snapshotEvents) {
    if (!merged.has(event.id)) merged.set(event.id, event);
  }
  for (const event of bufferedEvents) {
    merged.set(event.id, event);
  }
  return [...merged.values()];
}

export function shouldReconnectAfterClose(acknowledged, accessKeyValid, completedByServer) {
  return !completedByServer && (acknowledged || accessKeyValid !== false);
}

export async function shouldReconnectCardStream(close, validateAccessKey) {
  if (close.completedByServer) return false;
  if (close.acknowledged) return true;
  try {
    return (await validateAccessKey()) !== false;
  } catch {
    return true;
  }
}

export function createLatestRequestTracker() {
  let generation = 0;
  return {
    next() {
      generation += 1;
      return generation;
    },
    isCurrent(requestGeneration) {
      return requestGeneration === generation;
    },
    invalidate() {
      generation += 1;
    },
  };
}

export function sortSessionEvents(events) {
  return [...events].sort(
    (left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt),
  );
}
