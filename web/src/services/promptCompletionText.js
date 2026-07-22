export function activePromptCompletion(text, cursor = text.length) {
  const end = Math.max(0, Math.min(cursor, text.length));
  let start = end;
  while (start > 0 && !/\s/u.test(text[start - 1])) start -= 1;
  const token = text.slice(start, end);
  if (token.length === 0 || (token[0] !== '/' && token[0] !== '@')) return null;
  return {
    kind: token[0] === '/' ? 'command' : 'file',
    query: token.slice(1),
    start,
    end,
  };
}

export function filterSlashCommands(commands, query, hasThread) {
  return commands
    .filter((command) => hasThread || !command.requiresThread)
    .map((command, index) => ({ command, index, score: fuzzyScore(command.name.slice(1), query) }))
    .filter((item) => item.score !== null)
    .sort((left, right) => right.score - left.score || left.index - right.index)
    .map((item) => item.command);
}

export function applyPromptCompletion(text, range, value) {
  const suffix = text.slice(range.end);
  const separator = suffix.length === 0 || !/^\s/u.test(suffix) ? ' ' : '';
  return `${text.slice(0, range.start)}${value}${separator}${suffix}`;
}

export function formatFileMention(path) {
  if (!/\s/u.test(path)) return `@${path}`;
  return `@${JSON.stringify(path)}`;
}

export function promptMatchSegments(text, indices) {
  const matched = new Set(indices);
  const segments = [];
  for (let index = 0; index < text.length; index += 1) {
    const isMatched = matched.has(index);
    const last = segments.at(-1);
    if (last?.matched === isMatched) {
      last.text += text[index];
    } else {
      segments.push({ text: text[index], matched: isMatched });
    }
  }
  return segments;
}

function fuzzyScore(candidate, query) {
  const normalizedCandidate = candidate.toLocaleLowerCase();
  const normalizedQuery = query.toLocaleLowerCase();
  if (normalizedQuery.length === 0) return 0;
  let candidateIndex = 0;
  let previousMatch = -2;
  let score = 0;
  for (const character of normalizedQuery) {
    const match = normalizedCandidate.indexOf(character, candidateIndex);
    if (match < 0) return null;
    score += 10;
    if (match === previousMatch + 1) score += 6;
    if (match === 0 || /[-_/]/u.test(normalizedCandidate[match - 1])) score += 4;
    score -= match;
    previousMatch = match;
    candidateIndex = match + 1;
  }
  return score;
}
