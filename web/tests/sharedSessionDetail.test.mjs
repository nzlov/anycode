import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';
import {
  appendLiveEvent,
  createLatestRequestTracker,
  mergeRefreshedEvents,
  prependOlderEvents,
  shouldReconnectSubscription,
} from '../src/services/sessionEventTimeline.js';

const source = readFileSync(
  new URL('../src/composables/useSessionDetail.ts', import.meta.url),
  'utf8',
);
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
class SharingRequestError extends Error {
  constructor(message, status) {
    super(message);
    this.status = status;
  }
}
const flush = async () => {
  for (let i = 0; i < 30; i++) await Promise.resolve();
};
function deferred() {
  let resolve;
  const promise = new Promise((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
function setup(overrides = {}, shareId = 'share-1') {
  const calls = { subscriptions: 0, globalStarts: 0, directAppends: 0, submitted: [] };
  const timers = new Map();
  let timerID = 0;
  const session = {
    id: 'session-1',
    config: { codexModel: 'test-model' },
    artifactCount: 0,
    filesChanged: 0,
  };
  const api = {
    getSharedSession: async () => ({ sessionId: 'session-1' }),
    getGuestMessages: async () => [],
    getSession: async () => session,
    getPendingQuestionRequests: async () => [],
    getSessionTranscriptPage: async () => ({
      items: [],
      nodeUsage: [],
      pageInfo: { nextCursor: '' },
    }),
    submitSharedMessage: async (...args) => {
      calls.submitted.push(args);
      return { id: 'message-1', status: 'pending' };
    },
    ...overrides,
  };
  const dependencies = {
    vue: { ref: (value) => ({ value }) },
    '@/composables/useConfirmedSessionClose': {},
    '@/composables/useSessionUpdates': {
      useSessionUpdates: () => ({ start: () => calls.globalStarts++, stop() {} }),
    },
    '@/services/attachments': {},
    '@/services/sharing': { ...api, SharingRequestError },
    '@/services/graphqlClient': {},
    '@/services/sessionEventPaging': {},
    '@/services/sessions': {
      ...api,
      appendPrompt: async () => {
        calls.directAppends++;
      },
      subscribeSessionEvents: () => {
        calls.subscriptions++;
        return { unsubscribe() {} };
      },
    },
    '@/services/workflowApprovalReview': {},
    '@/services/sessionTimeline': api,
    '@/services/sessionEventTimeline': {
      appendLiveEvent,
      createLatestRequestTracker,
      mergeRefreshedEvents,
      prependOlderEvents,
      shouldReconnectSubscription,
    },
  };
  const module = { exports: {} };
  new Function('require', 'module', 'exports', 'setTimeout', 'clearTimeout', compiled)(
    (name) => {
      assert.ok(name in dependencies, name);
      return dependencies[name];
    },
    module,
    module.exports,
    (callback) => {
      const id = ++timerID;
      timers.set(id, callback);
      return id;
    },
    (id) => timers.delete(id),
  );
  return {
    detail: module.exports.useSessionDetail('session-1', shareId),
    calls,
    timers,
    session,
  };
}

test('owner detail keeps its subscriptions and direct append behavior', async () => {
  const { detail, calls, timers } = setup({}, '');
  detail.startLiveUpdates();
  detail.startLiveUpdates();
  assert.equal(calls.subscriptions, 1);
  assert.equal(calls.globalStarts, 1);
  assert.equal(timers.size, 0);
  await detail.appendDescription('owner prompt');
  assert.equal(calls.directAppends, 1);
  assert.equal(calls.submitted.length, 0);
  detail.stopLiveUpdates();
});

test('shared detail polls only its session and starts no owner subscriptions', async () => {
  const { detail, calls, timers, session } = setup();
  detail.startLiveUpdates();
  detail.startLiveUpdates();
  await flush();
  assert.equal(detail.session.value, session);
  assert.equal(detail.sharedActive.value, true);
  assert.equal(timers.size, 1);
  assert.equal(calls.subscriptions, 0);
  assert.equal(calls.globalStarts, 0);
  detail.stopLiveUpdates();
  detail.stopLiveUpdates();
  assert.equal(timers.size, 0);
  assert.equal(detail.sharedActive.value, false);
});

test('shared sends retain individual pending requests and never append directly', async () => {
  const { detail, calls } = setup();
  detail.startLiveUpdates();
  await flush();
  const file = { name: 'notes.txt' };
  assert.equal(await detail.submitSharedDescription('first', [file]), true);
  assert.equal(await detail.submitSharedDescription('second', []), true);
  assert.equal(calls.submitted.length, 2);
  assert.deepEqual(calls.submitted[0], ['share-1', 'first', { codexModel: 'test-model' }, [file]]);
  assert.equal(detail.sharedMessages.value.length, 2);
  assert.equal(detail.sharedMessages.value[0].status, 'pending');
  await assert.rejects(detail.appendDescription('bypass'), /逐条审核/);
  assert.equal(calls.directAppends, 0);
  assert.equal(await detail.submitSharedDescription('', []), false);
  detail.stopLiveUpdates();
  assert.equal(await detail.submitSharedDescription('after stop', []), false);
  assert.equal(calls.submitted.length, 2);
});

test('stopping discards outstanding session, transcript, questions and review replies', async () => {
  const state = deferred(),
    transcript = deferred(),
    messages = deferred(),
    questions = deferred();
  const { detail, timers } = setup({
    getSession: () => state.promise,
    getSessionTranscriptPage: () => transcript.promise,
    getGuestMessages: () => messages.promise,
    getPendingQuestionRequests: () => questions.promise,
  });
  detail.startLiveUpdates();
  await flush();
  detail.stopLiveUpdates();
  state.resolve({ id: 'late' });
  transcript.resolve({ items: [{ id: 'late' }], nodeUsage: [], pageInfo: { nextCursor: '' } });
  messages.resolve([{ id: 'late' }]);
  questions.resolve([{ id: 'late' }]);
  await flush();
  assert.equal(detail.session.value, null);
  assert.deepEqual(detail.events.value, []);
  assert.deepEqual(detail.sharedMessages.value, []);
  assert.deepEqual(detail.pendingQuestionRequests.value, []);
  assert.equal(timers.size, 0);
});

test('revocation disables sending and stops polling; transient errors can refresh again', async () => {
  let failure;
  const { detail, timers } = setup({
    getSharedSession: async () => {
      if (failure) throw failure;
      return {};
    },
  });
  detail.startLiveUpdates();
  await flush();
  const tick = async () => {
    const [id, callback] = timers.entries().next().value;
    timers.delete(id);
    callback();
    await flush();
  };
  failure = new Error('temporary network error');
  await tick();
  assert.equal(timers.size, 1);
  failure = new SharingRequestError('share revoked', 403);
  await tick();
  assert.equal(detail.sharedActive.value, false);
  assert.equal(detail.error.value, 'share revoked');
  assert.equal(timers.size, 0);
  assert.equal(await detail.submitSharedDescription('blocked', []), false);
});

test('a submission completing after unmount cannot overwrite review messages', async () => {
  const result = deferred();
  const { detail } = setup({ submitSharedMessage: () => result.promise });
  detail.startLiveUpdates();
  await flush();
  const sending = detail.submitSharedDescription('draft', []);
  detail.stopLiveUpdates();
  result.resolve({ id: 'late', status: 'pending' });
  assert.equal(await sending, false);
  assert.deepEqual(detail.sharedMessages.value, []);
});

test('sharing reuses the horizontal card and its authoritative detail state', () => {
  const page = readFileSync(new URL('../src/pages/SharedSessionPage.vue', import.meta.url), 'utf8');
  const card = readFileSync(
    new URL('../src/components/OverviewHorizontalConversation.vue', import.meta.url),
    'utf8',
  );
  const detail = readFileSync(
    new URL('../src/components/SessionDetailView.vue', import.meta.url),
    'utf8',
  );
  assert.match(page, /<OverviewHorizontalConversation/);
  assert.doesNotMatch(
    page,
    /getSessionTranscriptPage|SessionEventMessage|PromptComposer|setInterval|SessionDetailData/,
  );
  assert.match(card, /#header="\{ session: card, usage \}"/);
  assert.match(detail, /<slot name="header" :session="session"/);
  assert.match(detail, /<SessionShareManager\s+v-if="!shareId"/);
  assert.match(detail, /:allow-completions="false"/);
  assert.match(detail, /:readonly="!!shareId"/);
});
