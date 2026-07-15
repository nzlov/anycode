import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { fileDiffFromUnifiedDiff } from '../src/services/sessionFileChangeDiff.js';

test('file change unified diff is converted to DiffViewer line data', () => {
  const diff = fileDiffFromUnifiedDiff(
    'src/example.ts',
    'modified',
    '@@ -3,2 +3,3 @@\n const before = true;\n-oldValue();\n+newValue();\n+addedValue();',
  );

  assert.ok(diff);
  assert.deepEqual(diff.file, {
    path: 'src/example.ts',
    status: 'modified',
    additions: 2,
    deletions: 1,
  });
  assert.deepEqual(
    diff.hunks[0].lines.map((line) => [line.kind, line.oldLine, line.newLine]),
    [
      ['header', null, null],
      ['context', 3, 3],
      ['delete', 4, null],
      ['add', null, 4],
      ['add', null, 5],
    ],
  );
});

test('file change component delegates unified diff rendering to DiffViewer', () => {
  const source = readFileSync(
    new URL('../src/components/SessionFileChangeEvent.vue', import.meta.url),
    'utf8',
  );

  assert.match(source, /import DiffViewer from '@\/components\/DiffViewer\.vue'/);
  assert.match(source, /<DiffViewer[^>]*:file-diffs="diffFileChanges"/);
  assert.doesNotMatch(source, /DiffWorkspace|getSessionAllDiff|getSessionSingleDiff/);
  assert.doesNotMatch(source, /<pre v-if="change\.unifiedDiff"/);
  assert.match(source, /fileChangePresentations\.value\.flatMap\(\(\{ change, diff \}\)/);
});
