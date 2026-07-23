export function appendLiveEvent(events, event) {
  const existingIndex = events.findIndex((item) => item.id === event.id);
  if (existingIndex >= 0) {
    if (events[existingIndex] === event) return events;
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

export function mergeRefreshedEvents(events, refreshedEvents) {
  let next = events;
  for (const event of refreshedEvents) {
    next = appendLiveEvent(next, event);
  }
  if (next === events) return events;
  return [...next].sort((left, right) => left.orderKey.localeCompare(right.orderKey));
}

function mergeTimelineEvent(existing, incoming) {
  if (!existing?.group || !incoming?.group) {
    return mergeLiveEvent(existing, incoming);
  }
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

function mergeLiveEvent(existing, incoming) {
  const content = mergeLiveContent(existing.content, incoming.content, incoming.phase);
  return {
    ...existing,
    ...incoming,
    orderKey: existing.orderKey || incoming.orderKey,
    occurredAt: existing.occurredAt || incoming.occurredAt,
    content,
  };
}

function mergeLiveContent(existing, incoming, phase) {
  if (!existing || !incoming || existing.__typename !== incoming.__typename) return incoming;
  if (phase !== 'progress') return mergeContentSnapshot(existing, incoming);

  if (
    incoming.__typename === 'TranscriptMessageContent' ||
    incoming.__typename === 'TranscriptReasoningContent'
  ) {
    return { ...existing, ...incoming, text: `${existing.text ?? ''}${incoming.text ?? ''}` };
  }
  if (incoming.__typename === 'TranscriptCommandContent') {
    return {
      ...existing,
      ...incoming,
      commands: mergeCommandDeltas(existing.commands ?? [], incoming.commands ?? []),
    };
  }
  return mergeContentSnapshot(existing, incoming);
}

function mergeContentSnapshot(existing, incoming) {
  const content = { ...existing };
  for (const [key, value] of Object.entries(incoming)) {
    if (key === '__typename') continue;
    if (value !== '' && value !== null && value !== undefined) content[key] = value;
  }
  return content;
}

function mergeCommandDeltas(existing, incoming) {
  if (existing.length === 0) return incoming;
  if (incoming.length === 0) return existing;
  return existing.map((command, index) => {
    const delta = incoming[index];
    if (!delta) return command;
    return {
      ...command,
      ...delta,
      command: delta.command || command.command,
      workdir: delta.workdir || command.workdir,
      output: `${command.output ?? ''}${delta.output ?? ''}`,
      hasOutput: command.hasOutput || delta.hasOutput,
    };
  });
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
