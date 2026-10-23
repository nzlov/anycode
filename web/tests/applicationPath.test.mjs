import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';

const paths = readFileSync(
  new URL('../src/services/applicationPath.js', import.meta.url),
  'utf8',
).replaceAll('export ', '');
function setup(pathname, stored = {}) {
  const storage = new Map(Object.entries(stored));
  const window = {
    location: { pathname },
    localStorage: {
      getItem: (k) => storage.get(k) ?? null,
      setItem: (k, v) => storage.set(k, v),
      removeItem: (k) => storage.delete(k),
    },
  };
  return {
    storage,
    window,
    api: runInNewContext(
      `${paths}\n({applicationBasePath,applicationURL,applicationStorageKey,applicationAuthorizationHeader})`,
      { window },
    ),
  };
}

test('root deployment keeps URLs and credentials unchanged', () => {
  const { api } = setup('/');
  assert.equal(api.applicationBasePath(), '');
  assert.equal(api.applicationURL('/graphql'), '/graphql');
  assert.equal(api.applicationStorageKey('key'), 'key');
  assert.equal(api.applicationAuthorizationHeader(), 'authorization');
});

test('local preview scopes URLs, credentials and storage to its tunnel', () => {
  const { api } = setup('/tunnel/tunnel-one/');
  assert.equal(api.applicationURL('/graphql'), '/tunnel/tunnel-one/graphql');
  assert.equal(
    api.applicationURL('/files/image/preview?token=abc'),
    '/tunnel/tunnel-one/files/image/preview?token=abc',
  );
  assert.equal(
    api.applicationURL('/tunnel/tunnel-one/api/version'),
    '/tunnel/tunnel-one/api/version',
  );
  assert.equal(api.applicationURL('https://example.com/image'), 'https://example.com/image');
  assert.equal(api.applicationURL('//example.com/image'), '//example.com/image');
  assert.equal(api.applicationStorageKey('key'), 'key:/tunnel/tunnel-one');
  assert.equal(api.applicationAuthorizationHeader(), 'x-anycode-instance-authorization');
});

test('local instance login and logout never read or erase host credentials', () => {
  const { storage, window } = setup('/tunnel/one/', {
    'anycode.accessKey': 'host-key',
    ANYCODE_ACCESS_KEY: 'legacy-host-key',
  });
  const client = stripTypeScriptTypes(
    readFileSync(new URL('../src/services/graphqlClient.ts', import.meta.url), 'utf8'),
  )
    .replace(/^import[\s\S]*?from '[^']+';\n/gm, '')
    .replaceAll('export ', '')
    .replaceAll('import.meta.env.', 'env.');
  const api = runInNewContext(
    `${paths}\n${client}\n({getGraphQLAccessKey,setGraphQLAccessKey,clearGraphQLAccessKey})`,
    { window, env: {} },
  );
  assert.equal(api.getGraphQLAccessKey(), '');
  api.setGraphQLAccessKey('instance-key');
  assert.equal(api.getGraphQLAccessKey(), 'instance-key');
  window.location.pathname = '/tunnel/two/';
  assert.equal(api.getGraphQLAccessKey(), '');
  window.location.pathname = '/tunnel/one/';
  api.clearGraphQLAccessKey();
  assert.equal(storage.get('anycode.accessKey'), 'host-key');
  assert.equal(storage.get('ANYCODE_ACCESS_KEY'), 'legacy-host-key');
  assert.equal(api.getGraphQLAccessKey(), '');
});
