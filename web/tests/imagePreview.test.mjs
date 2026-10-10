import assert from 'node:assert/strict';
import { test } from 'node:test';
import { loadImagePreview } from '../src/services/imagePreview.ts';

globalThis.createImageBitmap = undefined;
globalThis.document = undefined;

const limit = 10 * 1024 * 1024;

test('images up to 10 MiB keep the original URL without decoding', async (t) => {
  t.mock.method(globalThis, 'fetch', () => assert.fail('unexpected fetch'));
  assert.deepEqual(await loadImagePreview('/original', new AbortController().signal, limit), {
    url: '/original',
    thumbnail: false,
  });
});

test('unknown sizes use actual response bytes and preserve small images', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response(new Blob(['original'])));
  const result = await loadImagePreview('/original', new AbortController().signal);
  assert.equal(result.thumbnail, false);
  // Native blob URL fetch verifies the original content is preserved.
  t.mock.restoreAll();
  assert.equal(await (await fetch(result.url)).text(), 'original');
  URL.revokeObjectURL(result.url);
});

test('large images get proportional thumbnails and release decoded resources', async (t) => {
  let closed = false;
  let drawn;
  const canvas = {
    width: 0,
    height: 0,
    getContext: () => ({
      drawImage: (...args) => {
        drawn = args.slice(1);
      },
    }),
    toBlob: (callback) => callback(new Blob(['thumbnail'], { type: 'image/webp' })),
  };
  t.mock.method(
    globalThis,
    'fetch',
    async () => new Response(new Blob([new Uint8Array(limit + 1)])),
  );
  t.mock.property(globalThis, 'createImageBitmap', async () => ({
    width: 4000,
    height: 8000,
    close: () => {
      closed = true;
    },
  }));
  t.mock.property(globalThis, 'document', { createElement: () => canvas });
  const result = await loadImagePreview('/original', new AbortController().signal);
  assert.equal(result.thumbnail, true);
  assert.deepEqual(drawn, [0, 0, 1024, 2048]);
  assert.equal(closed, true);
  assert.equal(canvas.width, 0);
  t.mock.restoreAll();
  assert.equal(await (await fetch(result.url)).text(), 'thumbnail');
  URL.revokeObjectURL(result.url);
});

test('cancellation during decoding closes bitmap and publishes no thumbnail', async (t) => {
  const controller = new AbortController();
  let closed = false;
  t.mock.method(
    globalThis,
    'fetch',
    async () => new Response(new Blob([new Uint8Array(limit + 1)])),
  );
  t.mock.property(globalThis, 'createImageBitmap', async () => {
    controller.abort();
    return {
      width: 4000,
      height: 8000,
      close: () => {
        closed = true;
      },
    };
  });
  await assert.rejects(loadImagePreview('/original', controller.signal), { name: 'AbortError' });
  assert.equal(closed, true);
});

test('HTTP failures remain visible instead of creating a broken thumbnail', async (t) => {
  t.mock.method(globalThis, 'fetch', async () => new Response('', { status: 403 }));
  await assert.rejects(loadImagePreview('/original', new AbortController().signal), /HTTP 403/);
});
