import {
  AnyCodeGraphQLError,
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { shouldReconnectSubscription } from '@/services/sessionEventTimeline';
import { subscribeSessionUpdates, type SessionUpdateEvent } from '@/services/sessions';

interface SessionUpdateHandlers {
  onData: (update: SessionUpdateEvent) => void;
  onReconnect?: () => void;
  onError?: (error: Error) => void;
}

export function useSessionUpdates(handlers: SessionUpdateHandlers) {
  let stopped = true;
  let generation = 0;
  let subscription: { unsubscribe: () => void } | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;

  function start() {
    if (!stopped) return;
    stopped = false;
    open();
  }

  function stop() {
    stopped = true;
    generation += 1;
    subscription?.unsubscribe();
    subscription = null;
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  }

  function open(reconnecting = false) {
    if (stopped) return;
    const currentGeneration = ++generation;
    subscription?.unsubscribe();
    subscription = subscribeSessionUpdates({
      onStart: () => {
        if (currentGeneration !== generation || stopped) return;
        if (reconnecting) handlers.onReconnect?.();
      },
      onData: (update) => {
        if (currentGeneration === generation) handlers.onData(update);
      },
      onError: (error) => {
        if (currentGeneration !== generation) return;
        handlers.onError?.(error);
        if (!(error instanceof AnyCodeGraphQLError && error.code === 'auth_failed')) {
          scheduleReconnect();
        }
      },
      onClose: (close) => {
        if (currentGeneration === generation) {
          void handleClose(close, currentGeneration);
        }
      },
    });
  }

  function scheduleReconnect() {
    if (stopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      open(true);
    }, 1500);
  }

  async function handleClose(close: GraphQLSubscriptionClose, currentGeneration: number) {
    const reconnect = await shouldReconnectSubscription(close, () =>
      verifyGraphQLAccessKey(getGraphQLAccessKey()),
    );
    if (stopped || currentGeneration !== generation) return;
    if (reconnect) scheduleReconnect();
  }

  return { start, stop };
}
