import { marked } from 'marked';
import type { FileDiff } from './diff';

export interface PreviewLine {
  content: string;
  kind: 'context' | 'add' | 'delete';
  oldLine: number | null;
  newLine: number | null;
}

export function contentLanguage(filename: string) {
  const name = filename.split('/').pop()?.toLowerCase() ?? '';
  const extension = name.split('.').pop() ?? '';
  const languages: Record<string, string> = {
    js: 'javascript',
    jsx: 'javascript',
    mjs: 'javascript',
    cjs: 'javascript',
    ts: 'typescript',
    tsx: 'typescript',
    vue: 'xml',
    html: 'xml',
    htm: 'xml',
    xml: 'xml',
    css: 'css',
    scss: 'scss',
    less: 'less',
    json: 'json',
    jsonl: 'json',
    go: 'go',
    py: 'python',
    rb: 'ruby',
    rs: 'rust',
    java: 'java',
    kt: 'kotlin',
    c: 'c',
    h: 'c',
    cpp: 'cpp',
    hpp: 'cpp',
    cs: 'csharp',
    swift: 'swift',
    php: 'php',
    sh: 'bash',
    bash: 'bash',
    zsh: 'bash',
    fish: 'bash',
    ps1: 'powershell',
    sql: 'sql',
    yaml: 'yaml',
    yml: 'yaml',
    toml: 'ini',
    ini: 'ini',
    conf: 'ini',
    dockerfile: 'dockerfile',
    makefile: 'makefile',
    env: 'bash',
    graphql: 'graphql',
    gql: 'graphql',
    diff: 'diff',
    patch: 'diff',
  };
  return languages[extension] ?? 'markdown';
}

export function diffPatch(diff: FileDiff) {
  if (diff.unifiedDiff !== undefined) return diff.unifiedDiff;
  const path = diff.file.path;
  const lines = [`diff --git a/${path} b/${path}`];
  lines.push(`--- ${diff.file.status === 'added' ? '/dev/null' : `a/${path}`}`);
  lines.push(`+++ ${diff.file.status === 'deleted' ? '/dev/null' : `b/${path}`}`);
  for (const hunk of diff.hunks) {
    lines.push(hunk.header);
    lines.push(...hunk.lines.filter((line) => line.kind !== 'header').map((line) => line.content));
  }
  return lines.join('\n') + '\n';
}

export function diffPreviewSections(diff: FileDiff): PreviewLine[][] {
  return diff.hunks.map((hunk) =>
    hunk.lines
      .filter((line) => line.kind !== 'header')
      .map((line) => ({
        ...line,
        kind: line.kind as PreviewLine['kind'],
        content: line.content.slice(1).replace(/\r$/, ''),
      })),
  );
}

export function diffPatchLines(diff: FileDiff) {
  let oldLine = 0;
  let newLine = 0;
  let inHunk = false;
  return diffPatch(diff)
    .split('\n')
    .map((text) => {
      const header = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(text);
      if (header) {
        oldLine = Number(header[1]);
        newLine = Number(header[2]);
        inHunk = true;
      }
      if (!header && inHunk && /^[ +-]/.test(text)) {
        const prefix = text[0]!;
        const line = prefix === '-' ? oldLine : newLine;
        const revision = prefix === '-' ? ('old' as const) : ('new' as const);
        if (prefix !== '+') oldLine++;
        if (prefix !== '-') newLine++;
        return { text: text.slice(1), prefix, line, revision };
      }
      return { text, prefix: '', line: undefined, revision: undefined };
    });
}

export function markdownPreviewBlocks(lines: PreviewLine[]) {
  const source = lines.map((line) => line.content).join('\n');
  const tokens = marked.lexer(source);
  const blocks: {
    text: string;
    kind: PreviewLine['kind'];
    language?: string;
    lines?: PreviewLine[];
  }[] = [];
  let offset = 0;
  function add(raw: string, start: number, language?: string) {
    const startLine = source.slice(0, start).split('\n').length - 1;
    const count = raw.replace(/\n+$/, '').split('\n').length;
    const rows = lines.slice(startLine, startLine + count);
    if (!raw.trim()) return;
    const changedFence = rows.some(
      (line) => line.kind !== 'context' && /^\s*(`{3,}|~{3,})/.test(line.content),
    );
    if (language !== undefined && !changedFence) {
      const fenced = /^\s*(`{3,}|~{3,})/.exec(raw);
      const codeRows = fenced
        ? rows.slice(1, /^\s*(`{3,}|~{3,})\s*$/.test(rows.at(-1)?.content ?? '') ? -1 : undefined)
        : rows;
      blocks.push({
        text: codeRows.map((line) => line.content).join('\n'),
        kind: 'context',
        language,
        lines: codeRows,
      });
      return;
    }
    const hasAdd = rows.some((line) => line.kind === 'add');
    const hasDelete = rows.some((line) => line.kind === 'delete');
    if (hasDelete)
      blocks.push({
        text: rows
          .filter((line) => line.kind !== 'add')
          .map((line) => line.content)
          .join('\n'),
        kind: 'delete',
      });
    if (hasAdd)
      blocks.push({
        text: rows
          .filter((line) => line.kind !== 'delete')
          .map((line) => line.content)
          .join('\n'),
        kind: 'add',
      });
    if (!hasAdd && !hasDelete) blocks.push({ text: raw, kind: 'context' });
  }
  for (const token of tokens) {
    const start = source.indexOf(token.raw, offset);
    if (start < 0) continue;
    if (token.type === 'list') {
      let itemOffset = start;
      for (const item of token.items) {
        const itemStart = source.indexOf(item.raw, itemOffset);
        add(item.raw, itemStart);
        itemOffset = itemStart + item.raw.length;
      }
    } else
      add(
        token.raw,
        start,
        token.type === 'code' && token.lang?.toLowerCase() !== 'mermaid'
          ? token.lang || 'plaintext'
          : undefined,
      );
    offset = start + token.raw.length;
  }
  // Preserve reference links when blocks are rendered independently.
  const definitions = Object.entries(tokens.links)
    .map(
      ([label, link]) =>
        `[${label}]: <${link.href}>${link.title ? ` ${JSON.stringify(link.title)}` : ''}`,
    )
    .join('\n');
  if (definitions) for (const block of blocks) if (!block.lines) block.text += '\n\n' + definitions;
  return blocks;
}
