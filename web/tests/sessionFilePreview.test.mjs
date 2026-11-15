import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { stripTypeScriptTypes } from 'node:module';
import { test } from 'node:test';
import { setImmediate } from 'node:timers/promises';
import { runInNewContext } from 'node:vm';
import { computed, nextTick, reactive, ref, watch } from 'vue';

const source = stripTypeScriptTypes(
  readFileSync(new URL('../src/components/SessionFilePreview.vue', import.meta.url), 'utf8').match(
    /<script setup lang="ts">([\s\S]*?)<\/script>/,
  )[1],
).replace(/^import[\s\S]*?;\n/gm, '');
const file = (id = 'image') => ({
  id,
  filename: `${id}.png`,
  previewKind: 'image',
  previewUrl: `/files/${id}/preview`,
  size: 42,
});

function preview({ observer = true, token } = {}) {
  const props = reactive({ file: file() });
  const requests = [];
  const revoked = [];
  let mounted, unmount, notify;
  let disconnected = false;
  const api = runInNewContext(`${source}; ({ previewRoot, imageURL, activated });`, {
    computed,
    ref,
    watch,
    AbortController,
    DOMException,
    defineProps: () => props,
    withDefaults: (value) => value,
    defineAsyncComponent: () => ({}),
    onMounted: (callback) => {
      mounted = callback;
    },
    onBeforeUnmount: (callback) => {
      unmount = callback;
    },
    IntersectionObserver: observer
      ? class {
          constructor(callback) {
            notify = callback;
          }
          observe(element) {
            assert.ok(element);
          }
          disconnect() {
            disconnected = true;
          }
        }
      : undefined,
    URL: { revokeObjectURL: (url) => revoked.push(url) },
    requestSessionFilePreviewURL: (value, signal) => {
      requests.push({ id: value.id, signal });
      return token ? token(value) : Promise.resolve(value.previewUrl);
    },
    loadImagePreview: async (url) => ({ url: `blob:${url}`, thumbnail: false }),
  });
  api.previewRoot.value = {};
  mounted();
  return {
    api,
    props,
    requests,
    revoked,
    unmount,
    visible: (value) => notify([{ isIntersecting: value }]),
    disconnected: () => disconnected,
  };
}
const settle = async () => {
  await nextTick();
  await setImmediate();
};

test('offscreen previews wait for visibility and retain the same file across rerenders and scrolling', async () => {
  const p = preview();
  assert.equal(p.requests.length, 0);
  p.props.file = file();
  await settle();
  assert.equal(p.requests.length, 0);
  p.visible(true);
  await settle();
  assert.equal(p.requests.length, 1);
  const url = p.api.imageURL.value;
  p.props.file = { ...file(), filename: 'renamed.png' };
  await settle();
  p.visible(false);
  p.visible(true);
  await settle();
  assert.equal(p.requests.length, 1);
  assert.equal(p.api.imageURL.value, url);
  p.unmount();
  assert.equal(p.disconnected(), true);
  assert.deepEqual(p.revoked, [url]);
});

test('changing an offscreen file clears the old preview and loads the new file when shown', async () => {
  const p = preview();
  p.visible(true);
  await settle();
  p.visible(false);
  p.props.file = file('replacement');
  await settle();
  assert.equal(p.api.imageURL.value, '');
  assert.equal(p.requests.length, 1);
  p.visible(true);
  await settle();
  assert.equal(p.requests.length, 2);
  assert.match(p.api.imageURL.value, /replacement/);
  p.props.file = { ...file('replacement'), previewUrl: '/files/replacement/preview?v=2' };
  await settle();
  assert.equal(p.requests.length, 3);
  p.props.file = { ...p.props.file, createdAt: '2026-11-14T10:00:00Z' };
  await settle();
  assert.equal(p.requests.length, 4);
  p.unmount();
});

test('replaced or unmounted requests cannot publish a stale preview', async () => {
  const pending = [];
  const p = preview({ token: () => new Promise((resolve) => pending.push(resolve)) });
  p.visible(true);
  p.props.file = file('new');
  await settle();
  assert.equal(p.requests[0].signal.aborted, true);
  pending[0]('/files/image/preview');
  await settle();
  assert.equal(p.api.imageURL.value, '');
  p.unmount();
  assert.equal(p.requests[1].signal.aborted, true);
  pending[1]('/files/new/preview');
  await settle();
  assert.equal(p.api.imageURL.value, '');
});

test('browsers without IntersectionObserver still load previews', async () => {
  const p = preview({ observer: false });
  await settle();
  assert.equal(p.requests.length, 1);
  assert.match(p.api.imageURL.value, /image/);
  p.unmount();
});
