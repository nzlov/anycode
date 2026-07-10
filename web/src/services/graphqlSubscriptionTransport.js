export function createGraphQLSubscriptionTransport({
  createSocket,
  connectionInitPayload,
  connectionAckTimeoutMs = 5000,
}) {
  let connection = null;
  let nextOperationID = 0;

  return {
    subscribe(operation) {
      if (!connection || connection.closed) {
        connection = createConnection();
      }
      return connection.subscribe(operation);
    },
    reset() {
      connection?.disconnect();
    },
  };

  function createConnection() {
    const socket = createSocket();
    const operations = new Map();
    let connectionAckTimer = null;
    const state = {
      acknowledged: false,
      closed: false,
      subscribe(operation) {
        const id = `subscription-${++nextOperationID}`;
        operations.set(id, { ...operation, started: false });
        if (state.acknowledged) startOperation(id);
        return {
          unsubscribe() {
            const active = operations.get(id);
            if (!active) return;
            operations.delete(id);
            if (active.started && socket.readyState === 1) {
              send({ id, type: 'complete' });
            }
            closeIfIdle();
          },
        };
      },
      disconnect() {
        if (state.closed) return;
        const active = [...operations.values()];
        operations.clear();
        detach();
        for (const operation of active) {
          operation.onClose?.({
            event: null,
            acknowledged: state.acknowledged,
            completedByServer: false,
          });
        }
        socket.close();
      },
    };

    socket.addEventListener('open', () => {
      send({ type: 'connection_init', payload: connectionInitPayload() });
      if (connectionAckTimeoutMs > 0) {
        connectionAckTimer = setTimeout(() => {
          if (!state.closed && !state.acknowledged) {
            socket.close(4408, 'Connection acknowledgement timeout');
          }
        }, connectionAckTimeoutMs);
      }
    });
    socket.addEventListener('message', (event) => {
      const message = parseMessage(event.data);
      if (!message) return;
      if (message.type === 'connection_ack') {
        state.acknowledged = true;
        clearConnectionAckTimer();
        for (const id of operations.keys()) startOperation(id);
        return;
      }
      if (message.type === 'ping') {
        send({ type: 'pong' });
        return;
      }
      if (typeof message.id !== 'string') return;
      const operation = operations.get(message.id);
      if (!operation) return;
      if (message.type === 'next') {
        operation.onNext(message.payload);
        return;
      }
      if (message.type === 'error') {
        operations.delete(message.id);
        operation.onError?.(new Error(subscriptionErrorMessage(message.payload)));
        closeIfIdle();
        return;
      }
      if (message.type === 'complete') {
        operations.delete(message.id);
        const idle = operations.size === 0;
        if (idle) detach();
        operation.onClose?.({
          event: null,
          acknowledged: state.acknowledged,
          completedByServer: true,
        });
        if (idle) socket.close();
      }
    });
    socket.addEventListener('error', () => {
      const error = new Error('GraphQL subscription connection failed');
      for (const operation of operations.values()) operation.onError?.(error);
    });
    socket.addEventListener('close', (event) => {
      if (state.closed) return;
      detach();
      const active = [...operations.values()];
      operations.clear();
      for (const operation of active) {
        operation.onClose?.({
          event,
          acknowledged: state.acknowledged,
          completedByServer: false,
        });
      }
    });

    return state;

    function startOperation(id) {
      const operation = operations.get(id);
      if (!operation || operation.started) return;
      operation.started = true;
      send({
        id,
        type: 'subscribe',
        payload: {
          query: operation.query,
          variables: operation.variables,
          operationName: operation.operationName,
        },
      });
    }

    function closeIfIdle() {
      if (operations.size !== 0) return;
      detach();
      socket.close();
    }

    function detach() {
      clearConnectionAckTimer();
      state.closed = true;
      if (connection === state) connection = null;
    }

    function clearConnectionAckTimer() {
      if (connectionAckTimer === null) return;
      clearTimeout(connectionAckTimer);
      connectionAckTimer = null;
    }

    function send(message) {
      socket.send(JSON.stringify(message));
    }
  }
}

function parseMessage(raw) {
  if (typeof raw !== 'string') return null;
  try {
    const message = JSON.parse(raw);
    return message && typeof message === 'object' ? message : null;
  } catch {
    return null;
  }
}

function subscriptionErrorMessage(payload) {
  if (Array.isArray(payload)) {
    return payload
      .map((item) =>
        item && typeof item.message === 'string' ? item.message : 'Subscription error',
      )
      .join('; ');
  }
  return 'GraphQL subscription failed';
}
