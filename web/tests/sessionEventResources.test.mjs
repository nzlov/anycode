import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import {
  matchChangedFilePath,
  parseSessionEventResourceReference,
} from '../src/services/sessionEventResourceReference.ts';

test('event resources distinguish authenticated files, artifacts, and workspace paths', () => {
  assert.deepEqual(parseSessionEventResourceReference('/files/artifact.123/preview', 'session-1'), {
    kind: 'session-file',
    fileId: 'artifact.123',
  });
  assert.deepEqual(
    parseSessionEventResourceReference(
      '/data/attachments/outputs/session-1/screens/home.png',
      'session-1',
    ),
    { kind: 'artifact', logicalPath: 'screens/home.png' },
  );
  assert.deepEqual(
    parseSessionEventResourceReference('/worktrees/session-1/web/App.vue:42', 'session-1'),
    {
      kind: 'workspace',
      path: '/worktrees/session-1/web/App.vue',
    },
  );
  assert.deepEqual(parseSessionEventResourceReference('reports/result.txt', 'session-1'), {
    kind: 'workspace',
    path: 'reports/result.txt',
  });
  assert.equal(
    parseSessionEventResourceReference('https://example.com/result.txt', 'session-1'),
    null,
  );
  assert.equal(parseSessionEventResourceReference('../outside.txt', 'session-1'), null);
});

test('absolute workspace links select the longest matching changed path', () => {
  assert.equal(
    matchChangedFilePath('/worktrees/session-1/test/src/App.vue', [
      'src/App.vue',
      'test/src/App.vue',
    ]),
    'test/src/App.vue',
  );
  assert.equal(matchChangedFilePath('web/App.vue', ['web/App.vue']), 'web/App.vue');
  assert.equal(matchChangedFilePath('/worktrees/session-1/README.md', ['web/App.vue']), null);
});

test('event stream routes local markdown and authenticated images through modal viewers', () => {
  const markdown = readFileSync(
    new URL('../src/components/MarkdownContent.vue', import.meta.url),
    'utf8',
  );
  const images = readFileSync(
    new URL('../src/components/SessionEventImages.vue', import.meta.url),
    'utf8',
  );
  const detail = readFileSync(
    new URL('../src/pages/SessionDetailPage.vue', import.meta.url),
    'utf8',
  );

  assert.match(markdown, /useSessionEventResourceOpener/);
  assert.match(markdown, /dataset\.eventResource/);
  assert.doesNotMatch(images, /:src="image\.src"/);
  assert.match(images, /<a[\s\S]*:href="image\.src"/);
  assert.match(images, /if \(!resourceOpener\?\.\(image\.src,[\s\S]*event\.preventDefault\(\)/);
  assert.match(images, /\.event-images__link\s*{[^}]*display:\s*inline/s);
  assert.doesNotMatch(images, /<q-icon|grid-template-columns|min-height:\s*40px/);
  assert.match(markdown, /const anchor = document\.createElement\('a'\)/);
  assert.doesNotMatch(markdown, /markdown-content__image-link/);
  assert.match(detail, /getSessionDiffFiles/);
  assert.match(detail, /eventDiffState\.value = \{ mode: 'single', filePath \}/);
  assert.match(detail, /resolveSessionArtifacts/);
  assert.match(detail, /<SessionFilePreview v-else :file="eventResourceFile"/);
  assert.match(detail, /class="event-resource-dialog app-content-dialog"/);
});

test('content-only diff workspaces preserve the requested diff mode', () => {
  const source = readFileSync(
    new URL('../src/components/DiffWorkspace.vue', import.meta.url),
    'utf8',
  );

  assert.match(
    source,
    /const workspaceMode = computed<DiffMode>\(\(\) => props\.modelValue\.mode\);/,
  );
  assert.doesNotMatch(source, /showFileNavigation \? props\.modelValue\.mode : 'all'/);
});
