import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';
import { nextTick, ref, watch } from 'vue';

const source = readFileSync(new URL('../src/components/ShareMessageNotifications.vue', import.meta.url), 'utf8');
const compiled = ts.transpileModule(source.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1], {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const flush = async () => {
  for (let i = 0; i < 30; i++) await Promise.resolve();
  await nextTick();
};
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => resolve = r);
  return { promise, resolve };
};
const message = (id, status = 'pending') => ({
  id, status, sessionId: `session-${id}`, createdAt: '2026-11-16T02:00:00Z',
  content: { body: `message ${id}`, config: {}, attachments: [] },
});
class SharingRequestError extends Error {
  constructor(message, status) { super(message); this.status = status; }
}

function setup(overrides = {}) {
  const calls = { pending: 0, details: [], reviews: [] };
  const unmounts = [], mounts = [], watchers = [];
  let handlers, active = false;
  const dependencies = {
    vue: {
      ref, watch: (...args) => watchers.push(watch(...args)),
      onMounted: (fn) => mounts.push(fn), onUnmounted: (fn) => unmounts.push(fn),
    },
    '@/composables/useSharingUpdates': {
      useSharingUpdates: (getSessionId, callbacks) => {
        assert.equal(getSessionId(), null);
        handlers = callbacks;
        return {
          start() { active = true; handlers.onReset(); },
          stop() { active = false; handlers.onReset(); },
        };
      },
    },
    '@/services/sharing': {
      SharingRequestError,
      listPendingShareMessages: async () => { calls.pending++; return []; },
      getShareMessage: async (id) => { calls.details.push(id); return message(id); },
      reviewShareMessage: async (id, approved) => { calls.reviews.push({ id, approved }); return message(id, approved ? 'approved' : 'rejected'); },
      ...overrides,
    },
  };
  const state = new Function('require', 'exports', compiled + '\nreturn { messages, reviewOpen, error, busy, refresh, review };')(
    (name) => { assert.ok(name in dependencies, name); return dependencies[name]; }, {},
  );
  mounts.forEach((fn) => fn());
  return {
    ...state, calls,
    send(eventType, fields = {}) { if (active) handlers.onData({ eventType, sessionId: null, ...fields }); },
    reconnect() { handlers.onReset(); handlers.onData({ eventType: 'sharing.ready' }); },
    unmount() { unmounts.forEach((fn) => fn()); watchers.forEach((fn) => fn()); },
  };
}

test('global notifications wait for ready and persist after closing the review dialog', async () => {
  const s = setup({ listPendingShareMessages: async () => [message('1'), message('2')] });
  assert.equal(s.messages.value.length, 0);
  s.send('sharing.ready');
  await flush();
  assert.equal(s.messages.value.length, 2);
  s.reviewOpen.value = true;
  s.reviewOpen.value = false;
  await flush();
  assert.equal(s.messages.value.length, 2);
  assert.doesNotMatch(source, /setInterval|setTimeout/);
  s.unmount();
});

test('new messages fetch by ID, deduplicate delivery and ignore link changes', async () => {
  const result = deferred();
  let reads = 0;
  const s = setup({ getShareMessage: async () => { reads++; return result.promise; } });
  s.send('sharing.ready');
  await flush();
  s.send('sharing.link_created');
  s.send('sharing.message_submitted', { messageId: '1' });
  s.send('sharing.message_submitted', { messageId: '1' });
  result.resolve(message('1'));
  await flush();
  assert.equal(reads, 1);
  assert.equal(s.calls.pending, 1);
  assert.deepEqual(s.messages.value.map(m => m.id), ['1']);
  s.send('sharing.message_status_changed', { messageId: '1', status: 'rejected' });
  await flush();
  assert.equal(s.messages.value.length, 0);
  s.unmount();
});

test('a message reviewed while its detail is loading cannot reappear as pending', async () => {
  const result = deferred();
  const s = setup({ getShareMessage: () => result.promise });
  s.send('sharing.message_submitted', { messageId: '1' });
  s.send('sharing.message_status_changed', { messageId: '1', status: 'approved' });
  result.resolve(message('1'));
  await flush();
  assert.equal(s.messages.value.length, 0);
  s.unmount();
});

test('pending snapshot cannot resurrect a message reviewed while it was loading', async () => {
  const result = deferred();
  let reads = 0;
  const s = setup({ listPendingShareMessages: async () => ++reads === 1 ? result.promise : [] });
  s.send('sharing.ready');
  s.send('sharing.message_status_changed', { messageId: '1', status: 'rejected' });
  result.resolve([message('1')]);
  await flush();
  assert.equal(reads, 2);
  assert.equal(s.messages.value.length, 0);
  s.unmount();
});

test('quick review handles messages individually and closes only after the last review', async () => {
  const s = setup({ listPendingShareMessages: async () => [message('1'), message('2')] });
  s.send('sharing.ready');
  await flush();
  s.reviewOpen.value = true;
  await s.review('1', true);
  assert.deepEqual(s.messages.value.map(m => m.id), ['2']);
  assert.equal(s.reviewOpen.value, true);
  await s.review('2', false);
  await flush();
  assert.deepEqual(s.calls.reviews, [{ id: '1', approved: true }, { id: '2', approved: false }]);
  assert.equal(s.messages.value.length, 0);
  assert.equal(s.reviewOpen.value, false);
  s.unmount();
});

test('review errors keep actionable messages and conflicts query current status', async () => {
  let failure = new Error('network unavailable');
  const s = setup({
    listPendingShareMessages: async () => [message('1')],
    reviewShareMessage: async () => { throw failure; },
    getShareMessage: async () => message('1', 'approved'),
  });
  s.send('sharing.ready');
  await flush();
  await s.review('1', true);
  assert.equal(s.messages.value.length, 1);
  assert.equal(s.error.value, 'network unavailable');
  assert.equal(s.busy.value, '');
  failure = new SharingRequestError('already reviewed', 409);
  await s.review('1', false);
  assert.equal(s.messages.value.length, 0);
  s.unmount();
});

test('reconnect and unmount discard outstanding requests and reset review busy state', async () => {
  const stale = deferred(), reviewed = deferred();
  const s = setup({ getShareMessage: () => stale.promise, reviewShareMessage: () => reviewed.promise });
  s.send('sharing.message_submitted', { messageId: '1' });
  const reviewing = s.review('1', true);
  s.reconnect();
  await flush();
  assert.equal(s.busy.value, '');
  stale.resolve(message('1'));
  reviewed.resolve(message('1', 'approved'));
  await reviewing;
  await flush();
  assert.equal(s.messages.value.length, 0);
  s.unmount();
  s.send('sharing.message_submitted', { messageId: '2' });
  await flush();
  assert.equal(s.messages.value.length, 0);
});
