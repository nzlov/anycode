import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';
import { runInNewContext } from 'node:vm';

const source = stripTypeScriptTypes(
  readFileSync(new URL('../src/services/sessionFiles.ts', import.meta.url), 'utf8'),
)
  .replace(/^import .*graphqlClient';\n/m, '')
  .replaceAll('export ', '');

function setup(response) {
  const requests = [];
  const actions = [];
  const anchor = {
    click() {
      actions.push('click');
    },
    remove() {
      actions.push('remove');
    },
  };
  const download = runInNewContext(`${source}\ndownloadSessionFile;`, {
    URL,
    Headers,
    window: { location: { origin: 'https://anycode.test' } },
    getGraphQLAccessKey: () => 'secret',
    fetch: async (url, options) => {
      requests.push({ url, options });
      return response;
    },
    document: {
      createElement(tag) {
        assert.equal(tag, 'a');
        return anchor;
      },
      body: {
        appendChild(element) {
          assert.equal(element, anchor);
          actions.push('append');
        },
      },
    },
  });
  return { download, requests, anchor, actions };
}

for (const target of [
  { url: '/files/file-1/download', endpoint: '/files/file-1/download-token' },
  {
    url: '/api/sessions/session-1/workspace-file?path=src%2F%E6%8A%A5%E5%91%8A+%23.zip&download=1',
    endpoint:
      '/api/sessions/session-1/workspace-file/download-token?path=src%2F%E6%8A%A5%E5%91%8A+%23.zip&download=1',
  },
]) {
  test(`browser downloads ${target.url} without fetching file contents`, async () => {
    const signedURL = `${target.url}${target.url.includes('?') ? '&' : '?'}token=short-lived`;
    const context = setup({
      ok: true,
      json: async () => ({ url: signedURL }),
      blob() {
        assert.fail('file contents must go directly to the browser');
      },
    });
    await context.download({ filename: '报告 #.zip', downloadUrl: target.url });
    assert.equal(context.requests.length, 1);
    assert.equal(context.requests[0].url, target.endpoint);
    assert.equal(context.requests[0].options.method, 'POST');
    assert.equal(context.requests[0].options.headers.get('authorization'), 'Bearer secret');
    assert.equal(context.anchor.href, `https://anycode.test${signedURL}`);
    assert.equal(context.anchor.download, '报告 #.zip');
    assert.equal(context.anchor.rel, 'noreferrer');
    assert.deepEqual(context.actions, ['append', 'click', 'remove']);
  });
}

test('failed token requests do not start a download', async () => {
  const context = setup({ ok: false, status: 401 });
  await assert.rejects(context.download({ downloadUrl: '/files/file-1/download' }), /HTTP 401/);
  assert.deepEqual(context.actions, []);
});

test('download credentials are never sent to an external or unrelated endpoint', async () => {
  for (const downloadUrl of [
    'https://outside.test/files/file-1/download',
    '//outside.test/files/file-1/download',
    '/graphql',
    '',
  ]) {
    const context = setup({ ok: true });
    await assert.rejects(context.download({ downloadUrl }), /文件下载地址无效/);
    assert.deepEqual(context.requests, []);
    assert.deepEqual(context.actions, []);
  }
});

test('invalid token responses do not navigate the browser', async () => {
  for (const url of [
    null,
    'https://outside.test/files/file-1/download',
    '/files/file-2/download',
    '/files/file-1/preview',
  ]) {
    const context = setup({ ok: true, json: async () => ({ url }) });
    await assert.rejects(
      context.download({ downloadUrl: '/files/file-1/download' }),
      /文件下载凭据响应无效/,
    );
    assert.deepEqual(context.actions, []);
  }
});
