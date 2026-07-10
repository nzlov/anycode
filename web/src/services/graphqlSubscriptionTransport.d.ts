export interface GraphQLTransportClose {
  event: CloseEvent | null;
  acknowledged: boolean;
  completedByServer: boolean;
}

export interface GraphQLTransportOperation {
  query: string;
  variables?: Record<string, unknown>;
  operationName?: string;
  onNext: (payload: unknown) => void;
  onError?: (error: Error) => void;
  onClose?: (close: GraphQLTransportClose) => void;
}

export type GraphQLTransportSocket = Pick<
  WebSocket,
  'readyState' | 'addEventListener' | 'send' | 'close'
>;

export function createGraphQLSubscriptionTransport(options: {
  createSocket: () => GraphQLTransportSocket;
  connectionInitPayload: () => Record<string, unknown>;
}): {
  subscribe(operation: GraphQLTransportOperation): { unsubscribe: () => void };
  reset(): void;
};
