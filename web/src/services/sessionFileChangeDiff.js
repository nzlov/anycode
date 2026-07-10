// GLUE: transcript file-change events carry unified diff text while DiffViewer consumes structured hunks.
// Remove when the transcript API exposes the shared FileDiff shape directly.
export function fileDiffFromUnifiedDiff(path, status, unifiedDiff) {
  const hunks = [];
  let current = null;
  let additions = 0;
  let deletions = 0;

  for (const line of String(unifiedDiff || '').split('\n')) {
    if (line.startsWith('@@')) {
      if (current) hunks.push(current);
      const starts = hunkStarts(line);
      current = {
        id: String(hunks.length),
        header: line,
        oldStart: starts.oldStart,
        newStart: starts.newStart,
        canExpandBefore: false,
        canExpandAfter: false,
        lines: [diffLine(`${hunks.length}:header`, 'header', line, null, null)],
        nextOldLine: starts.oldStart,
        nextNewLine: starts.newStart,
      };
      continue;
    }
    if (!current || line === '' || line === '\\ No newline at end of file') continue;

    const kind = line.startsWith('+') ? 'add' : line.startsWith('-') ? 'delete' : 'context';
    const oldLine = kind === 'add' ? null : current.nextOldLine;
    const newLine = kind === 'delete' ? null : current.nextNewLine;
    current.lines.push(
      diffLine(`${current.id}:${current.lines.length - 1}`, kind, line, oldLine, newLine),
    );
    if (kind !== 'add') current.nextOldLine += 1;
    if (kind !== 'delete') current.nextNewLine += 1;
    if (kind === 'add') additions += 1;
    if (kind === 'delete') deletions += 1;
  }
  if (current) hunks.push(current);
  if (hunks.length === 0) return null;

  const normalizedHunks = hunks.map((hunk) => ({
    id: hunk.id,
    header: hunk.header,
    oldStart: hunk.oldStart,
    newStart: hunk.newStart,
    canExpandBefore: hunk.canExpandBefore,
    canExpandAfter: hunk.canExpandAfter,
    lines: hunk.lines,
  }));
  return {
    file: { path, status, additions, deletions },
    hunks: normalizedHunks,
    lines: normalizedHunks.flatMap((hunk) => hunk.lines),
  };
}

function hunkStarts(header) {
  const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(header);
  return {
    oldStart: Number(match?.[1] ?? 0),
    newStart: Number(match?.[2] ?? 0),
  };
}

function diffLine(id, kind, content, oldLine, newLine) {
  return { id, kind, content, oldLine, newLine };
}
