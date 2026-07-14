export function reduceTranscriptEvents(events) {
  const orderedEvents = [...events].sort(compareTranscriptEvents);
  const items = [];
  const correlated = new Map();

  for (const event of orderedEvents) {
    if (!event.correlationId || event.phase === 'standalone') {
      items.push(toTranscriptItem(event));
      continue;
    }

    const existing = correlated.get(event.correlationId);
    if (!existing) {
      const item = toTranscriptItem(event, event.correlationId);
      correlated.set(event.correlationId, item);
      items.push(item);
      continue;
    }

    existing.content = mergeTranscriptContent(existing.content, event.content);
    existing.phase = event.phase;
    existing.sourceEventIds.push(event.id);
    if (event.phase === 'started' && event.orderKey < existing.orderKey) {
      existing.orderKey = event.orderKey;
      existing.occurredAt = event.occurredAt;
    }
  }

  return items.sort(compareTranscriptEvents);
}

function toTranscriptItem(event, id = event.id) {
  return {
    ...event,
    id,
    content: cloneContent(event.content),
    sourceEventIds: [event.id],
  };
}

function mergeTranscriptContent(current, incoming) {
  if (current.__typename === 'TranscriptCommandContent') {
    if (incoming.__typename === 'TranscriptCommandContent') {
      return {
        ...mergeDefined(current, incoming),
        commands: incoming.commands?.length ? incoming.commands : current.commands,
      };
    }
  }

  if (current.__typename === 'TranscriptToolContent' && incoming.__typename === 'TranscriptToolContent') {
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
  if (Array.isArray(content)) return content.map(cloneContent);
  if (content === null || typeof content !== 'object') return content;

  return Object.fromEntries(
    Object.entries(content).map(([key, value]) => [key, cloneContent(value)]),
  );
}

function compareTranscriptEvents(left, right) {
  return left.orderKey.localeCompare(right.orderKey);
}
