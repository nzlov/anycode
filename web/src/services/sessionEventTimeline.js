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

export async function shouldReconnectSubscription(close, validateAccessKey) {
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

export function createKeyedLatestRequestTracker() {
  const generations = new Map();
  return {
    next(key) {
      const generation = (generations.get(key) ?? 0) + 1;
      generations.set(key, generation);
      return generation;
    },
    isCurrent(key, requestGeneration) {
      return generations.get(key) === requestGeneration;
    },
    invalidate(key) {
      generations.set(key, (generations.get(key) ?? 0) + 1);
    },
    clear() {
      generations.clear();
    },
  };
}
