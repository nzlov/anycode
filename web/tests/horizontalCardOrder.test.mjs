import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { test } from 'node:test';
import ts from 'typescript';
import { computed, effectScope, nextTick, ref } from 'vue';

const require = createRequire(import.meta.url);
const source = readFileSync(
  new URL('../src/composables/useHorizontalCardOrder.ts', import.meta.url),
  'utf8',
);
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const storageKey = 'anycode.overview.horizontal-order.v1';

function setup(t, stored = null, initialIds = ['a', 'b', 'c']) {
  const originalWindow = globalThis.window;
  const storage = new Map(stored === null ? [] : [[storageKey, stored]]);
  const frames = new Map();
  const listeners = new Map();
  let frameId = 0;
  globalThis.window = {
    localStorage: {
      getItem: (key) => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
    },
    requestAnimationFrame: (callback) => {
      frames.set(++frameId, callback);
      return frameId;
    },
    cancelAnimationFrame: (id) => frames.delete(id),
    addEventListener: (name, callback) => listeners.set(name, callback),
    removeEventListener: (name) => listeners.delete(name),
  };
  let cleanup;
  const module = { exports: {} };
  new Function('require', 'module', 'exports', compiled)(
    (name) =>
      name === 'vue'
        ? {
            ...require('vue'),
            onUnmounted: (callback) => {
              cleanup = callback;
            },
          }
        : require(name),
    module,
    module.exports,
  );
  const cards = ref(initialIds.map((id) => ({ id, title: id })));
  const enabled = ref(true);
  const scope = effectScope();
  const model = scope.run(() =>
    module.exports.useHorizontalCardOrder(
      computed(() => cards.value),
      computed(() => enabled.value),
    ),
  );
  let visibleIds = null;
  let capture = null;
  const section = {
    scrollLeft: 0,
    contains: () => true,
    setPointerCapture: (id) => {
      capture = id;
    },
    hasPointerCapture: (id) => capture === id,
    releasePointerCapture: () => {
      capture = null;
    },
    getBoundingClientRect: () => ({ left: 0, right: 900, top: 0, bottom: 600, width: 900 }),
    querySelectorAll: () =>
      (visibleIds ?? model.orderedCards.value.map((card) => card.id)).map((id, index) => ({
        dataset: { sessionId: id },
        getBoundingClientRect: () => ({
          left: index * 332 - section.scrollLeft,
          right: (index + 1) * 332 - section.scrollLeft,
          width: 332,
        }),
      })),
  };
  model.sectionRef.value = section;
  function event(id, x = 20, extra = {}) {
    const handle = {
      closest: () => ({ dataset: { sessionId: id } }),
      focus() {},
      scrollIntoView() {},
    };
    return {
      target: { closest: () => handle },
      button: 0,
      isPrimary: true,
      pointerId: 1,
      clientX: x,
      clientY: 20,
      preventDefault() {},
      stopPropagation() {},
      ...extra,
    };
  }
  const ids = () => model.orderedCards.value.map((card) => card.id);
  t.after(() => {
    cleanup();
    scope.stop();
    globalThis.window = originalWindow;
  });
  return {
    model,
    cards,
    enabled,
    storage,
    frames,
    listeners,
    section,
    event,
    ids,
    hide: (ids) => {
      visibleIds = ids;
    },
  };
}

test('dragging changes order only after dropping and persists it for reload', (t) => {
  const { model, event, ids, storage } = setup(t);
  model.beginDrag(event('a'));
  model.continueDrag(event('a', 990));
  assert.equal(model.draggedId.value, 'a');
  assert.deepEqual(model.dropTarget.value, { id: 'c', side: 'after' });
  assert.deepEqual(ids(), ['a', 'b', 'c']);
  model.finishDrag(event('a', 990));
  assert.deepEqual(ids(), ['b', 'c', 'a']);
  assert.deepEqual(JSON.parse(storage.get(storageKey)), ['b', 'c', 'a']);
  assert.equal(model.draggedId.value, '');
  assert.match(model.announcement.value, /a/);
});

test('saved order survives updates, scoped routes and cards being added or removed', async (t) => {
  const { model, cards, event, ids, storage } = setup(t, '["c","outside-scope","b","a"]');
  assert.deepEqual(ids(), ['c', 'b', 'a']);
  cards.value = [
    { id: 'new' },
    ...cards.value.map((card) => ({ ...card, updatedTime: 'changed' })),
  ];
  assert.deepEqual(ids(), ['new', 'c', 'b', 'a']);
  model.onKeydown(event('a', 20, { key: 'ArrowLeft' }));
  assert.deepEqual(ids(), ['new', 'c', 'a', 'b']);
  assert.deepEqual(JSON.parse(storage.get(storageKey)), ['new', 'c', 'outside-scope', 'a', 'b']);
  cards.value = cards.value.filter((card) => card.id !== 'c');
  await nextTick();
  assert.deepEqual(ids(), ['new', 'a', 'b']);
});

test('dropping before an earlier card moves left while dropping in place does not save', (t) => {
  const { model, event, ids, storage } = setup(t);
  model.beginDrag(event('c', 700));
  model.continueDrag(event('c', 20));
  model.finishDrag(event('c', 20));
  assert.deepEqual(ids(), ['c', 'a', 'b']);
  const saved = storage.get(storageKey);
  model.beginDrag(event('c', 20));
  model.continueDrag(event('c', 100));
  model.finishDrag(event('c', 100));
  assert.equal(storage.get(storageKey), saved);
});

test('keyboard movement uses visible neighbors without losing hidden cards', async (t) => {
  const { model, event, ids, hide } = setup(t);
  hide(['a', 'c']);
  model.onKeydown(event('a', 20, { key: 'ArrowRight' }));
  await nextTick();
  assert.deepEqual(ids(), ['b', 'c', 'a']);
});

test('clicks, secondary pointers and pointer movements below the threshold do not reorder', (t) => {
  const { model, event, ids, storage } = setup(t);
  model.beginDrag(event('a', 20, { button: 2 }));
  model.continueDrag(event('a', 990));
  assert.equal(model.draggedId.value, '');
  model.beginDrag(event('a', 20, { isPrimary: false }));
  model.continueDrag(event('a', 990));
  assert.equal(model.draggedId.value, '');
  model.beginDrag(event('a'));
  model.continueDrag(event('a', 24));
  model.finishDrag(event('a', 24));
  assert.deepEqual(ids(), ['a', 'b', 'c']);
  assert.equal(storage.has(storageKey), false);
});

test('Escape, pointer cancellation, leaving the section and disabling the layout cancel the drag', async (t) => {
  const { model, event, ids, frames, enabled, listeners, storage } = setup(t);
  for (const cancel of [
    () => model.onKeydown(event('a', 20, { key: 'Escape' })),
    () => model.cancelDrag(),
    () => model.finishDrag(event('a', 990, { clientY: 800 })),
    () => listeners.get('blur')(),
    async () => {
      enabled.value = false;
      await nextTick();
    },
  ]) {
    model.beginDrag(event('a'));
    model.continueDrag(event('a', 990));
    await cancel();
    assert.deepEqual(ids(), ['a', 'b', 'c']);
    assert.equal(model.draggedId.value, '');
    assert.equal(model.dropTarget.value, null);
    assert.equal(frames.size, 0);
    assert.equal(listeners.size, 0);
    assert.equal(storage.has(storageKey), false);
  }
});

test('dragging near the edge scrolls and cancels animation when the source disappears', async (t) => {
  const { model, event, frames, section, cards } = setup(t);
  model.beginDrag(event('a'));
  model.continueDrag(event('a', 890));
  const [id, callback] = [...frames.entries()][0];
  frames.delete(id);
  callback();
  assert.ok(section.scrollLeft > 0);
  cards.value = cards.value.filter((card) => card.id !== 'a');
  await nextTick();
  assert.equal(model.draggedId.value, '');
  assert.equal(frames.size, 0);
});

test('invalid saved data is ignored and storage failures leave reordering usable', (t) => {
  const { model, event, ids } = setup(t, '{invalid');
  window.localStorage.setItem = () => {
    throw new Error('storage unavailable');
  };
  model.onKeydown(event('c', 20, { key: 'ArrowLeft' }));
  assert.deepEqual(ids(), ['a', 'c', 'b']);
});
