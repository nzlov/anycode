export function reduceSessionTimelineEvents(events) {
  const orderedEvents = [...events].sort(compareTimelineEvents);
  const items = [];
  const correlated = new Map();

  for (const event of orderedEvents) {
    if (!event.correlationId || event.phase === 'standalone') {
      items.push(toTimelineItem(event));
      continue;
    }

    const existing = correlated.get(event.correlationId);
    if (!existing) {
      const item = toTimelineItem(event, event.correlationId);
      correlated.set(event.correlationId, item);
      items.push(item);
      continue;
    }

    existing.content = mergeTimelineContent(existing.content, event.content);
    existing.phase = event.phase;
    existing.sourceEventIds.push(event.id);
    if (event.phase === 'started' && event.orderKey < existing.orderKey) {
      existing.orderKey = event.orderKey;
      existing.occurredAt = event.occurredAt;
    }
  }

  return items.sort(compareTimelineEvents);
}

function toTimelineItem(event, id = event.id) {
  return {
    ...event,
    id,
    content: cloneContent(event.content),
    sourceEventIds: [event.id],
  };
}

function mergeTimelineContent(current, incoming) {
  if (current.__typename === 'SessionCommandContent') {
    if (incoming.__typename === 'SessionCommandContent') {
      return mergeDefined(current, incoming);
    }
  }

  if (current.__typename === 'SessionToolContent' && incoming.__typename === 'SessionToolContent') {
    return {
      ...current,
      qualifiedName: incoming.qualifiedName || current.qualifiedName,
      category: incoming.category || current.category,
      input: mergeStructuredText(current.input, incoming.input),
      output: mergeStructuredText(current.output, incoming.output),
      images: mergeImages(current.images, incoming.images),
    };
  }

  if (current.__typename === incoming.__typename) return mergeDefined(current, incoming);
  return current;
}

function mergeDefined(current, incoming) {
  const result = { ...current };
  for (const [key, value] of Object.entries(incoming)) {
    if (key === '__typename') continue;
    if (value !== '' && value !== null && value !== undefined) result[key] = value;
  }
  return result;
}

function mergeStructuredText(current, incoming) {
  if (!incoming?.text) return current;
  return incoming;
}

function mergeImages(current = [], incoming = []) {
  const images = new Map(current.map((image) => [`${image.src}:${image.detail}`, image]));
  for (const image of incoming) images.set(`${image.src}:${image.detail}`, image);
  return [...images.values()];
}

function cloneContent(content) {
  return structuredClone(content);
}

function compareTimelineEvents(left, right) {
  return left.orderKey.localeCompare(right.orderKey);
}
