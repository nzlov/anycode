export function appendLiveEvent(events, event) {
  const existingIndex = events.findIndex((item) => item.id === event.id);
  if (existingIndex >= 0) {
    if (!event.group) return events;
    const next = [...events];
    next[existingIndex] = mergeTimelineEvent(events[existingIndex], event);
    return next;
  }
  return [...events, event];
}

export function prependOlderEvents(events, olderEvents) {
  const next = [...events];
  const indexes = new Map(next.map((event, index) => [event.id, index]));
  const older = [];
  for (const event of olderEvents) {
    const existingIndex = indexes.get(event.id);
    if (existingIndex === undefined) {
      older.push(event);
      continue;
    }
    next[existingIndex] = mergeTimelineEvent(event, next[existingIndex]);
  }
  if (older.length === 0 && next.every((event, index) => event === events[index])) return events;
  return [...older, ...next];
}

export function mergeSnapshotEvents(snapshotEvents, currentEvents, bufferedEvents) {
  const merged = new Map(currentEvents.map((event) => [event.id, event]));
  for (const event of snapshotEvents) {
    const current = merged.get(event.id);
    merged.set(event.id, current ? mergeTimelineEvent(event, current) : event);
  }
  for (const event of bufferedEvents) {
    const current = merged.get(event.id);
    merged.set(event.id, current ? mergeTimelineEvent(current, event) : event);
  }
  return [...merged.values()];
}

function mergeTimelineEvent(existing, incoming) {
  if (!existing?.group || !incoming?.group) return incoming;
  const members = new Map(existing.group.members.map((member) => [member.id, member]));
  for (const member of incoming.group.members) members.set(member.id, member);
  const mergedMembers = [...members.values()].sort((left, right) =>
    left.orderKey.localeCompare(right.orderKey),
  );
  const existingFirst = existing.orderKey.localeCompare(incoming.orderKey) <= 0;
  const first = existingFirst ? existing : incoming;
  return {
    ...first,
    group: {
      ...existing.group,
      ...incoming.group,
      count: mergedMembers.length,
      members: mergedMembers,
    },
  };
}

export function shouldReconnectAfterClose(acknowledged, accessKeyValid, completedByServer) {
  if (completedByServer) return acknowledged;
  return acknowledged || accessKeyValid !== false;
}

export async function shouldReconnectCardStream(close, validateAccessKey) {
  if (close.completedByServer) return close.acknowledged;
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

export function sortTranscriptEvents(events) {
  return [...events].sort((left, right) => left.orderKey.localeCompare(right.orderKey));
}
