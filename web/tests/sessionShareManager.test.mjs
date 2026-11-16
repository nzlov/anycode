import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';
import { computed, nextTick, reactive, ref, watch } from 'vue';

const source = readFileSync(
  new URL('../src/components/SessionShareManager.vue', import.meta.url),
  'utf8',
).match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const updatesCompiled = ts.transpileModule(readFileSync(
  new URL('../src/composables/useSharingUpdates.ts', import.meta.url), 'utf8',
), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;

const flush = async () => {
  for (let i = 0; i < 30; i++) await Promise.resolve();
  await nextTick();
};
const deferred = () => {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
};
class AnyCodeGraphQLError extends Error {
  constructor(code) {
    super(code);
    this.code = code;
  }
}

function setup(overrides = {}) {
  const props = reactive({ sessionId: 'session-1' });
  const open = ref(false);
  const subscriptions = [];
  const timers = new Map();
  const watchers = [];
  const unmount = [];
  const mounted = [];
  const calls = { links: [], messages: [], pending: [], unsubscribed: 0 };
  let timerID = 0;
  const setTimer = (callback) => {
    timers.set(++timerID, callback);
    return timerID;
  };
  const clearTimer = (id) => timers.delete(id);
  const api = {
    createSessionShare: async () => ({ id: 'link-2', url: 'https://example.test/share' }),
    revokeSessionShare: async () => {},
    reviewShareMessage: async () => ({}),
    listSessionShares: async (id) => {
      calls.links.push(id);
      return [{ id: 'link-1', claimedAt: null, revokedAt: null }];
    },
    listShareMessages: async (id) => {
      calls.messages.push(id);
      return [{ id: 'message-1', status: 'pending' }];
    },
    ...overrides,
    subscribeSharingUpdates: (sessionId, handlers) => {
      subscriptions.push({ sessionId, ...handlers });
      return { unsubscribe: () => calls.unsubscribed++ };
    },
  };
  const dependencies = {
    vue: {
      computed,
      ref,
      watch: (...args) => watchers.push(watch(...args)),
      onMounted: (callback) => mounted.push(callback),
      onUnmounted: (callback) => unmount.push(callback),
    },
    quasar: { copyToClipboard: async () => {}, Notify: { create() {} } },
    '@/services/sharing': api,
    '@/services/graphqlClient': { AnyCodeGraphQLError },
    '@/services/sessionEventTimeline': {
      shouldReconnectSubscription: async () => true,
    },
  };
  const updatesModule = { exports: {} };
  new Function('require', 'module', 'exports', 'setTimeout', 'clearTimeout', updatesCompiled)(
    (name) => dependencies[name], updatesModule, updatesModule.exports, setTimer, clearTimer,
  );
  dependencies['@/composables/useSharingUpdates'] = updatesModule.exports;
  const state = new Function(
    'require',
    'exports',
    'defineProps',
    'defineModel',
    'defineEmits',
    'setTimeout',
    'clearTimeout',
    'setInterval',
    compiled + '\nreturn { shares, messages, pending, loading, error, refresh, create, revoke, review };',
  )(
    (name) => {
      assert.ok(name in dependencies, name);
      return dependencies[name];
    },
    {},
    () => props,
    () => open,
    () => (name, value) => calls[name].push(value),
    setTimer,
    clearTimer,
    () => assert.fail('sharing must not start a polling interval'),
  );
  mounted.forEach((callback) => callback());
  return {
    ...state,
    open,
    props,
    calls,
    subscriptions,
    timers,
    send: (eventType, fields = {}, index = subscriptions.length - 1) => {
      subscriptions[index].onData({ sessionId: props.sessionId, eventType, ...fields });
    },
    unmount: () => {
      unmount.forEach((callback) => callback());
      watchers.forEach((stop) => stop());
    },
    reconnect: async () => {
      const [id, callback] = timers.entries().next().value;
      timers.delete(id);
      callback();
      await flush();
    },
  };
}

test('idle cards subscribe without polling; queries wait for the server ready event', async () => {
  const s = setup();
  assert.equal(s.subscriptions.length, 1);
  assert.equal(s.calls.messages.length, 0);
  s.send('sharing.ready');
  await flush();
  assert.deepEqual(s.calls.messages, ['session-1']);
  assert.deepEqual(s.calls.links, []);
  assert.equal(s.pending.value, 1);
  assert.equal(s.timers.size, 0);
  s.send('sharing.message_submitted', { sessionId: 'other' });
  await flush();
  assert.equal(s.calls.messages.length, 1);
  s.open.value = true;
  await flush();
  assert.equal(s.calls.links.length, 1);
  s.open.value = false;
  await flush();
  assert.equal(s.timers.size, 0);
  s.unmount();
  assert.equal(s.calls.unsubscribed, 1);
});

test('message statuses and link timestamps update in place; new messages trigger one query', async () => {
  const s = setup();
  s.send('sharing.ready');
  s.open.value = true;
  await flush();
  const messageQueries = s.calls.messages.length;
  const linkQueries = s.calls.links.length;
  s.send('sharing.message_status_changed', { messageId: 'message-1', status: 'approved' });
  s.send('sharing.link_claimed', { shareId: 'link-1', occurredAt: 'claimed' });
  s.send('sharing.link_revoked', { shareId: 'link-1', occurredAt: 'revoked' });
  await flush();
  assert.equal(s.messages.value[0].status, 'approved');
  assert.equal(s.pending.value, 0);
  assert.equal(s.calls.pending.at(-1), 0);
  assert.equal(s.shares.value[0].claimedAt, 'claimed');
  assert.equal(s.shares.value[0].revokedAt, 'revoked');
  assert.equal(s.calls.messages.length, messageQueries);
  assert.equal(s.calls.links.length, linkQueries);
  s.open.value = false;
  await flush();
  s.send('sharing.link_created', { shareId: 'link-2' });
  s.send('sharing.message_submitted', { messageId: 'new-message' });
  await flush();
  assert.equal(s.calls.messages.length, messageQueries + 1);
  assert.equal(s.calls.links.length, linkQueries);
  s.unmount();
});

test('an event received during a query cannot be overwritten by its stale response', async () => {
  const result = deferred();
  let queries = 0;
  const s = setup({
    listShareMessages: async () => {
      if (++queries === 1) return [{ id: 'message-1', status: 'pending' }];
      if (queries === 2) return result.promise;
      return [{ id: 'message-1', status: 'sent' }, { id: 'message-2', status: 'pending' }];
    },
  });
  s.send('sharing.ready');
  await flush();
  s.send('sharing.message_submitted', { messageId: 'message-2' });
  await flush();
  s.send('sharing.message_status_changed', { messageId: 'message-1', status: 'sent' });
  assert.equal(s.messages.value[0].status, 'sent');
  result.resolve([{ id: 'message-1', status: 'pending' }]);
  await flush();
  assert.equal(queries, 3);
  assert.equal(s.messages.value[0].status, 'sent');
  assert.equal(s.messages.value.length, 2);
  s.unmount();
});

test('reconnect refreshes once after ready and discards callbacks from the old subscription', async () => {
  const s = setup();
  s.send('sharing.ready');
  await flush();
  await s.subscriptions[0].onClose({ acknowledged: true });
  await flush();
  assert.equal(s.timers.size, 1);
  await s.reconnect();
  assert.equal(s.subscriptions.length, 2);
  assert.equal(s.calls.messages.length, 1);
  s.send('sharing.message_submitted', {}, 0);
  s.send('sharing.ready');
  await flush();
  assert.equal(s.calls.messages.length, 2);
  assert.equal(s.calls.links.length, 0);
  assert.equal(s.timers.size, 0);
  s.unmount();
});

test('unmount and session changes discard outstanding queries and queued retries', async () => {
  const result = deferred();
  const s = setup({ listShareMessages: () => result.promise });
  s.send('sharing.ready');
  s.props.sessionId = 'session-2';
  await flush();
  assert.equal(s.subscriptions.length, 2);
  assert.equal(s.subscriptions[1].sessionId, 'session-2');
  result.resolve([{ id: 'old-session-message', status: 'pending' }]);
  await flush();
  assert.equal(s.messages.value.length, 0);
  s.subscriptions[1].onError(new Error('disconnected'));
  assert.equal(s.timers.size, 1);
  s.unmount();
  assert.equal(s.timers.size, 0);
  s.send('sharing.message_submitted');
  await flush();
  assert.equal(s.messages.value.length, 0);
});

test('authentication failures do not start a reconnect loop', () => {
  const s = setup();
  s.subscriptions[0].onError(new AnyCodeGraphQLError('auth_failed'));
  assert.equal(s.timers.size, 0);
  assert.equal(s.error.value, 'auth_failed');
  s.unmount();
});

test('local mutations use subscription notifications instead of querying both lists again', async () => {
  const s = setup();
  s.open.value = true;
  s.send('sharing.ready');
  await flush();
  const links = s.calls.links.length;
  const messages = s.calls.messages.length;
  await s.create();
  await s.revoke('link-1');
  await s.review('message-1', false);
  assert.equal(s.calls.links.length, links);
  assert.equal(s.calls.messages.length, messages);
  s.send('sharing.link_created', { shareId: 'link-2' });
  await flush();
  assert.equal(s.calls.links.length, links + 1);
  assert.equal(s.calls.messages.length, messages);
  s.unmount();
});
