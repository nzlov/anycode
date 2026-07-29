import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';

import { hasNewerRelease } from '../src/services/appVersionModel.js';

const layoutSource = readFileSync(
  new URL('../src/layouts/MainLayout.vue', import.meta.url),
  'utf8',
);
const workflowSource = readFileSync(
  new URL('../../.github/workflows/publish-image.yml', import.meta.url),
  'utf8',
);
const dockerfileSource = readFileSync(new URL('../../Dockerfile', import.meta.url), 'utf8');

test('release comparison accepts optional v prefixes', () => {
  assert.equal(hasNewerRelease('v1.2.3', '1.2.4'), true);
  assert.equal(hasNewerRelease('1.2.3', 'v1.2.3'), false);
  assert.equal(hasNewerRelease('v2.0.0', 'v1.9.9'), false);
});

test('git describe builds compare from their release base', () => {
  assert.equal(hasNewerRelease('v1.2.3-4-gabcdef0', 'v1.2.3'), false);
  assert.equal(hasNewerRelease('v1.2.3-4-gabcdef0+20260729T021913Z', 'v1.2.3'), false);
  assert.equal(hasNewerRelease('v1.2.3-4-gabcdef0', 'v1.3.0'), true);
});

test('a stable release supersedes its prerelease', () => {
  assert.equal(hasNewerRelease('v1.2.3-rc.1', 'v1.2.3'), true);
  assert.equal(hasNewerRelease('v1.2.3+build.5', 'v1.2.3'), false);
});

test('development and commit-only builds do not report unprovable updates', () => {
  assert.equal(hasNewerRelease('dev', 'v1.2.3'), false);
  assert.equal(hasNewerRelease('abcdef0', 'v1.2.3'), false);
});

test('container workflow injects git describe into the Go build', () => {
  assert.match(workflowSource, /git describe --tags --always --match 'v\[0-9\]\*'/);
  assert.match(workflowSource, /date -u \+'%Y%m%dT%H%M%SZ'/);
  assert.match(workflowSource, /ANYCODE_VERSION=\$\{\{ steps\.version\.outputs\.value \}\}/);
  assert.match(dockerfileSource, /-X main\.version=\$\{ANYCODE_VERSION\}/);
});

test('top-right menu exposes the current version and release notes', () => {
  assert.match(layoutSource, /aria-label="更多操作"[\s\S]*当前版本 \{\{ currentVersion \}\}/);
  assert.match(layoutSource, /<MarkdownContent :text="availableRelease\.body"/);
  assert.match(layoutSource, /aria-label="有新版本"/);
});
