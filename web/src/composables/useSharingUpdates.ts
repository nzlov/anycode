import {
  AnyCodeGraphQLError,
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { shouldReconnectSubscription } from '@/services/sessionEventTimeline';
import { subscribeSharingUpdates, type SharingUpdate } from '@/services/sharing';

export function useSharingUpdates(
  sessionId: () => string | null,
  handlers: {
    onData: (update: SharingUpdate) => void;
    onError: (error: Error) => void;
    onReset: () => void;
  },
) {
  let stopped = true;
  let generation = 0;
  let subscription: { unsubscribe: () => void } | undefined;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;

  function start() {
    if (!stopped) return;
    stopped = false;
    connect();
  }

  function stop() {
    stopped = true;
    generation++;
    subscription?.unsubscribe();
    subscription = undefined;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    handlers.onReset();
  }

  function connect() {
    if (stopped) return;
    const current = ++generation;
    subscription?.unsubscribe();
    handlers.onReset();
    subscription = subscribeSharingUpdates(sessionId(), {
      onData: (update) => {
        if (current === generation && !stopped) handlers.onData(update);
      },
      onError: (error) => {
        if (current !== generation || stopped) return;
        handlers.onError(error);
        if (!(error instanceof AnyCodeGraphQLError && error.code === 'auth_failed')) {
          scheduleReconnect();
        }
      },
      onClose: (close) => void handleClose(close, current),
    });
  }

  function scheduleReconnect() {
    if (stopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = undefined;
      connect();
    }, 1500);
  }

  async function handleClose(close: GraphQLSubscriptionClose, current: number) {
    if (current !== generation || stopped) return;
    const reconnect = await shouldReconnectSubscription(close, () =>
      verifyGraphQLAccessKey(getGraphQLAccessKey()),
    );
    if (current === generation && !stopped && reconnect) scheduleReconnect();
  }

  return { start, stop };
}
