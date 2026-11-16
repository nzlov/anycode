import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  contentLanguage,
  diffPatch,
  diffPatchLines,
  diffPreviewSections,
  markdownPreviewBlocks,
} from '../src/services/contentPreview.ts';
import { fileDiffFromUnifiedDiff } from '../src/services/sessionFileChangeDiff.js';

const parse = (patch, path = 'README.md', status = 'modified') =>
  fileDiffFromUnifiedDiff(path, status, patch);

test('file type decides Markdown rendering versus code highlighting', () => {
  for (const path of ['README.md', 'report.markdown', 'notes.txt', 'LICENSE'])
    assert.equal(contentLanguage(path), 'markdown');
  for (const [path, language] of [
    ['src/App.vue', 'xml'],
    ['sample.TS', 'typescript'],
    ['main.go', 'go'],
    ['Dockerfile', 'dockerfile'],
    ['.env', 'bash'],
    ['config.yaml', 'yaml'],
  ])
    assert.equal(contentLanguage(path), language);
});

test('source preserves the original event patch including metadata and missing final newline', () => {
  const patch =
    'diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -2 +2 @@\n-old\n+new\n\\ No newline at end of file';
  const diff = parse(patch);
  assert.equal(diffPatch(diff), patch);
  assert.equal(
    diffPatchLines(diff)
      .map((line) => line.prefix + line.text)
      .join('\n'),
    patch,
  );
  assert.deepEqual(
    diffPatchLines(diff)
      .filter((line) => line.line !== undefined)
      .map((line) => [line.text, line.line, line.revision]),
    [
      ['old', 2, 'old'],
      ['new', 2, 'new'],
    ],
  );
});

test('structured diffs produce patch text without duplicating hunk headers', () => {
  const diff = parse('@@ -1 +1 @@\n-old\n+new');
  delete diff.unifiedDiff;
  assert.equal(
    diffPatch(diff),
    'diff --git a/README.md b/README.md\n--- a/README.md\n+++ b/README.md\n@@ -1 +1 @@\n-old\n+new\n',
  );
});

test('separate hunks remain separate and never invent unrecorded context', () => {
  const diff = parse('@@ -4 +4 @@\n-old\n+new\n@@ -100 +100 @@\n-second\n+replacement');
  const sections = diffPreviewSections(diff);
  assert.equal(sections.length, 2);
  assert.deepEqual(
    sections.map((lines) => lines.map((line) => line.content)),
    [
      ['old', 'new'],
      ['second', 'replacement'],
    ],
  );
  assert.equal(sections[1][0].oldLine, 100);
});

test('Markdown changes preserve headings, task lists and fenced code as structured blocks', () => {
  const diff = parse(
    '@@ -1,9 +1,9 @@\n # Title\n \n-old **text**\n+new **text**\n \n - [x] keep\n-- [ ] pending\n+- [x] done\n \n ```ts\n-const interval = 30;\n+const interval = 10;\n ```',
  );
  const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
  assert.equal(blocks[0].text.trim(), '# Title');
  assert.ok(blocks.some((block) => block.kind === 'delete' && block.text === 'old **text**'));
  assert.ok(blocks.some((block) => block.kind === 'add' && block.text === 'new **text**'));
  assert.ok(
    blocks.some((block) => block.kind === 'context' && block.text.startsWith('- [x] keep')),
  );
  assert.ok(blocks.some((block) => block.kind === 'delete' && block.text === '- [ ] pending'));
  const code = blocks.find((block) => block.language === 'ts');
  assert.deepEqual(
    code.lines.map((line) => [line.content, line.kind]),
    [
      ['const interval = 30;', 'delete'],
      ['const interval = 10;', 'add'],
    ],
  );
});

test('tables retain complete Markdown structure in each changed version', () => {
  const diff = parse(
    '@@ -1,3 +1,3 @@\n | Item | Value |\n | --- | --- |\n-| Delay | 30 |\n+| Delay | 10 |',
  );
  const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
  assert.deepEqual(
    blocks.map((block) => block.kind),
    ['delete', 'add'],
  );
  assert.ok(blocks.every((block) => block.text.includes('| --- | --- |')));
  assert.ok(!blocks[0].text.includes('| Delay | 10 |'));
});

test('added and deleted files keep content colors and valid patch paths', () => {
  for (const [status, prefix, header] of [
    ['added', '+', '@@ -0,0 +1,2 @@'],
    ['deleted', '-', '@@ -1,2 +0,0 @@'],
  ]) {
    const diff = parse(`${header}\n${prefix}# Title\n${prefix}body`, 'README.md', status);
    const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
    assert.ok(blocks.every((block) => block.kind === (status === 'added' ? 'add' : 'delete')));
    delete diff.unifiedDiff;
    assert.ok(diffPatch(diff).includes(status === 'added' ? '--- /dev/null' : '+++ /dev/null'));
  }
});

test('reference links survive rendering Markdown blocks independently', () => {
  const diff = parse(
    '@@ -1,5 +1,5 @@\n # Links\n \n [Guide][guide]\n \n [guide]: https://example.com/docs "Guide"',
  );
  const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
  assert.ok(
    blocks.every((block) => block.text.includes('[guide]: <https://example.com/docs> "Guide"')),
  );
});

test('Mermaid changes keep diagram rendering and the source for each revision', () => {
  const diff = parse('@@ -1,3 +1,3 @@\n ```mermaid\n-graph LR; A-->B\n+graph LR; A-->C\n ```');
  const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
  assert.deepEqual(
    blocks.map((block) => block.kind),
    ['delete', 'add'],
  );
  assert.ok(blocks.every((block) => block.text.startsWith('```mermaid\n') && !block.lines));
});

test('changed fence languages render valid code blocks in both revisions', () => {
  const diff = parse('@@ -1,3 +1,3 @@\n-```javascript\n+```typescript\n const value = 1;\n ```');
  const blocks = markdownPreviewBlocks(diffPreviewSections(diff)[0]);
  assert.deepEqual(
    blocks.map((block) => block.kind),
    ['delete', 'add'],
  );
  assert.equal(blocks[0].text, '```javascript\nconst value = 1;\n```');
  assert.equal(blocks[1].text, '```typescript\nconst value = 1;\n```');
});
