import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

const source = stripTypeScriptTypes(
  readFileSync(new URL('../src-pwa/sw/custom-sw.ts', import.meta.url), 'utf8'),
)
  .replace(/^import[\s\S]*?from 'workbox-[^']+';\n/gm, '')
  .replaceAll('import.meta.env.', 'env.');

function navigationOptions() {
  let options;
  runInNewContext(source, {
    env: {
      QUASAR_PROD: true,
      QUASAR_PWA_FALLBACK_HTML: 'index.html',
      QUASAR_PWA_SERVICE_WORKER_REGEX: 'sw\\.js$',
    },
    self: { skipWaiting() {}, addEventListener() {}, __WB_MANIFEST: [] },
    clientsClaim() {},
    precacheAndRoute() {},
    cleanupOutdatedCaches() {},
    createHandlerBoundToURL() {
      return () => {};
    },
    NavigationRoute: class {
      constructor(_handler, value) {
        options = value;
      }
    },
    registerRoute() {},
  });
  return options;
}

const { denylist } = navigationOptions();
const excluded = (path) => denylist.some((pattern) => pattern.test(path));

for (const path of [
  '/files/file-1/preview?token=test',
  '/files/file-1/download?token=test',
  '/api/sessions/card-1/workspace-file?path=report.pdf&token=test',
  '/api/sessions/card-1/diff-media?path=report.pdf&version=new&token=test',
  '/tunnel/preview/report.pdf',
]) {
  test(`navigation fallback leaves ${path} to the server`, () => {
    assert.equal(excluded(path), true);
  });
}

for (const path of ['/', '/index.html', '/sessions/card-1', '/files-page']) {
  test(`navigation fallback still handles application path ${path}`, () => {
    assert.equal(excluded(path), false);
  });
}
