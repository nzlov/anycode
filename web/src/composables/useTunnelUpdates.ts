import {
  AnyCodeGraphQLError,
  getGraphQLAccessKey,
  verifyGraphQLAccessKey,
  type GraphQLSubscriptionClose,
} from '@/services/graphqlClient';
import { shouldReconnectSubscription } from '@/services/sessionEventTimeline';
import { subscribeTunnelUpdates, type TunnelCountUpdate } from '@/services/tunnels';

export function useTunnelUpdates(onData: (update: TunnelCountUpdate) => void) {
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
    if (reconnectTimer) clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }

  function open() {
    if (stopped) return;
    const currentGeneration = ++generation;
    subscription?.unsubscribe();
    subscription = subscribeTunnelUpdates({
      onData: (update) => {
        if (currentGeneration === generation) onData(update);
      },
      onError: (error) => {
        if (
          currentGeneration === generation &&
          !(error instanceof AnyCodeGraphQLError && error.code === 'auth_failed')
        ) {
          scheduleReconnect();
        }
      },
      onClose: (close) => {
        if (currentGeneration === generation) void handleClose(close, currentGeneration);
      },
    });
  }

  function scheduleReconnect() {
    if (stopped || reconnectTimer) return;
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null;
      open();
    }, 1500);
  }

  async function handleClose(close: GraphQLSubscriptionClose, currentGeneration: number) {
    const reconnect = await shouldReconnectSubscription(close, () =>
      verifyGraphQLAccessKey(getGraphQLAccessKey()),
    );
    if (!stopped && currentGeneration === generation && reconnect) scheduleReconnect();
  }

  return { start, stop };
}
