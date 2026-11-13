import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { runInNewContext } from 'node:vm';
import { test } from 'node:test';

function setup(hash = '#/share/first') {
  const storage = new Map([['anycode.accessKey', 'owner-secret']]);
  const window = {
    location: { hash, pathname: '/' },
    localStorage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
  };
  const pathSource = readFileSync(
    new URL('../src/services/applicationPath.js', import.meta.url),
    'utf8',
  ).replaceAll('export ', '');
  const shared = stripTypeScriptTypes(
    readFileSync(new URL('../src/services/sharedAccess.ts', import.meta.url), 'utf8'),
  )
    .replace(/^import[^;]+;\n/gm, '')
    .replaceAll('export ', '');
  const client = stripTypeScriptTypes(
    readFileSync(new URL('../src/services/graphqlClient.ts', import.meta.url), 'utf8'),
  )
    .replace(/^import[\s\S]*?from '[^']+';\n/gm, '')
    .replaceAll('export ', '')
    .replaceAll('import.meta.env.', 'env.');
  const api = runInNewContext(
    `${pathSource}\n${shared}\n${client}\n({currentShareId,shareCredential,saveShareCredential,sharedResourcePath,getGraphQLAccessKey})`,
    { window, env: {} },
  );
  return { api, window, storage };
}
test('shared pages never inherit owner credentials and isolate each recipient', () => {
  const { api, window, storage } = setup();
  assert.equal(api.getGraphQLAccessKey(), '');
  api.saveShareCredential('first', 'guest-one');
  assert.equal(api.getGraphQLAccessKey(), 'guest-one');
  window.location.hash = '#/share/second';
  assert.equal(api.getGraphQLAccessKey(), '');
  api.saveShareCredential('second', 'guest-two');
  assert.equal(api.getGraphQLAccessKey(), 'guest-two');
  window.location.hash = '#/share/first';
  assert.equal(api.getGraphQLAccessKey(), 'guest-one');
  assert.equal(
    api.sharedResourcePath('/files/file/preview'),
    '/api/shared/first/files/file/preview',
  );
  window.location.hash = '#/sessions/s';
  assert.equal(api.getGraphQLAccessKey(), 'owner-secret');
  assert.equal(storage.get('anycode.accessKey'), 'owner-secret');
});
test('invalid invitation IDs still cannot fall back to owner credentials', () => {
  const { api } = setup('#/share/invalid-id?token=bad');
  assert.equal(api.getGraphQLAccessKey(), '');
});
test('share browser credentials stay inside the hosting instance', () => {
  const { api, window } = setup();
  window.location.pathname = '/tunnel/one/';
  api.saveShareCredential('first', 'one');
  window.location.pathname = '/tunnel/two/';
  assert.equal(api.getGraphQLAccessKey(), '');
  window.location.pathname = '/tunnel/one/';
  assert.equal(api.getGraphQLAccessKey(), 'one');
});
