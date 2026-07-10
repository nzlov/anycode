import assert from 'node:assert/strict';
import { test } from 'node:test';

import { createGraphQLSubscriptionTransport } from '../src/services/graphqlSubscriptionTransport.js';

test('multiple subscriptions share one websocket and route messages by operation id', () => {
  const sockets = [];
  const transport = createGraphQLSubscriptionTransport({
    createSocket: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    connectionInitPayload: () => ({ Authorization: 'Bearer secret' }),
  });
  const received = [];

  const first = transport.subscribe({
    query: 'subscription First { first }',
    onNext: (payload) => received.push(['first', payload]),
  });
  const second = transport.subscribe({
    query: 'subscription Second { second }',
    onNext: (payload) => received.push(['second', payload]),
  });

  assert.equal(sockets.length, 1);
  const socket = sockets[0];
  socket.open();
  assert.deepEqual(socket.sent[0], {
    type: 'connection_init',
    payload: { Authorization: 'Bearer secret' },
  });

  socket.message({ type: 'connection_ack' });
  const subscriptions = socket.sent.filter((message) => message.type === 'subscribe');
  assert.equal(subscriptions.length, 2);
  assert.notEqual(subscriptions[0].id, subscriptions[1].id);

  socket.message({ id: subscriptions[1].id, type: 'next', payload: { data: { second: 2 } } });
  socket.message({ id: subscriptions[0].id, type: 'next', payload: { data: { first: 1 } } });
  assert.deepEqual(received, [
    ['second', { data: { second: 2 } }],
    ['first', { data: { first: 1 } }],
  ]);

  first.unsubscribe();
  assert.equal(socket.closed, false);
  second.unsubscribe();
  assert.equal(socket.closed, true);
});

test('connection close reports acknowledgement state to every active operation', () => {
  const socket = new FakeSocket();
  const closes = [];
  const transport = createGraphQLSubscriptionTransport({
    createSocket: () => socket,
    connectionInitPayload: () => ({}),
  });

  transport.subscribe({
    query: 'subscription First { first }',
    onClose: (close) => closes.push(close),
  });
  transport.subscribe({
    query: 'subscription Second { second }',
    onClose: (close) => closes.push(close),
  });
  socket.open();
  socket.message({ type: 'connection_ack' });
  socket.remoteClose(1006);

  assert.equal(closes.length, 2);
  assert.equal(
    closes.every((close) => close.acknowledged),
    true,
  );
  assert.equal(
    closes.every((close) => !close.completedByServer),
    true,
  );
});

test('server completion removes one operation without closing the shared connection', () => {
  const socket = new FakeSocket();
  const closes = [];
  const transport = createGraphQLSubscriptionTransport({
    createSocket: () => socket,
    connectionInitPayload: () => ({}),
  });

  transport.subscribe({
    query: 'subscription First { first }',
    onClose: (close) => closes.push(close),
  });
  const second = transport.subscribe({ query: 'subscription Second { second }' });
  socket.open();
  socket.message({ type: 'connection_ack' });
  const subscriptions = socket.sent.filter((message) => message.type === 'subscribe');

  socket.message({ id: subscriptions[0].id, type: 'complete' });

  assert.equal(closes.length, 1);
  assert.equal(closes[0].completedByServer, true);
  assert.equal(socket.closed, false);
  second.unsubscribe();
  assert.equal(socket.closed, true);
});

test('reset disconnects active operations and the next page opens a fresh connection', () => {
  const sockets = [];
  const closes = [];
  const transport = createGraphQLSubscriptionTransport({
    createSocket: () => {
      const socket = new FakeSocket();
      sockets.push(socket);
      return socket;
    },
    connectionInitPayload: () => ({}),
  });

  transport.subscribe({
    query: 'subscription First { first }',
    onClose: (close) => closes.push(close),
  });
  sockets[0].open();
  sockets[0].message({ type: 'connection_ack' });
  transport.reset();
  transport.subscribe({ query: 'subscription Second { second }' });

  assert.equal(closes.length, 1);
  assert.equal(closes[0].completedByServer, false);
  assert.equal(sockets[0].closed, true);
  assert.equal(sockets.length, 2);
});

class FakeSocket {
  static OPEN = 1;

  listeners = new Map();
  readyState = 0;
  sent = [];
  closed = false;

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  send(raw) {
    this.sent.push(JSON.parse(raw));
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.readyState = 3;
    this.emit('close', { code: 1000 });
  }

  open() {
    this.readyState = FakeSocket.OPEN;
    this.emit('open', {});
  }

  message(message) {
    this.emit('message', { data: JSON.stringify(message) });
  }

  remoteClose(code) {
    this.closed = true;
    this.readyState = 3;
    this.emit('close', { code });
  }

  emit(type, event) {
    for (const listener of this.listeners.get(type) ?? []) listener(event);
  }
}
