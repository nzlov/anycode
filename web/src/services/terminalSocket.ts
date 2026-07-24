import { getGraphQLAccessKey } from '@/services/graphqlClient';

interface TerminalSocketHandlers {
  onReady: () => void;
  onOutput: (data: Uint8Array) => void;
  onExit: () => void;
  onError: (message: string) => void;
  onConnectionChange?: (connected: boolean) => void;
}

export interface TerminalSocket {
  send(data: string): void;
  resize(cols: number, rows: number): void;
  acknowledge(bytes: number): void;
  disconnect(): void;
}

export function connectTerminal(
  sessionId: string,
  handlers: TerminalSocketHandlers,
): TerminalSocket {
  let socket: WebSocket | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  let reconnectAttempts = 0;
  let disconnected = false;
  let terminalEnded = false;
  let latestSize: { cols: number; rows: number } | null = null;
  const encoder = new TextEncoder();

  connect();

  return {
    send(data) {
      if (socket?.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
    },
    resize(cols, rows) {
      latestSize = { cols, rows };
      sendJSON({ type: 'resize', cols, rows });
    },
    acknowledge(bytes) {
      sendJSON({ type: 'ack', bytes });
    },
    disconnect() {
      disconnected = true;
      if (reconnectTimer !== null) clearTimeout(reconnectTimer);
      reconnectTimer = null;
      socket?.close(1000, 'terminal view closed');
      socket = null;
    },
  };

  function connect() {
    if (disconnected || terminalEnded) return;
    const current = new WebSocket(terminalWebSocketURL(sessionId));
    current.binaryType = 'arraybuffer';
    socket = current;
    current.addEventListener('open', () => {
      current.send(
        JSON.stringify({
          type: 'connection_init',
          authorization: bearerAuthorization(),
        }),
      );
    });
    current.addEventListener('message', (event) => {
      if (typeof event.data !== 'string') {
        if (event.data instanceof ArrayBuffer) {
          handlers.onOutput(new Uint8Array(event.data));
        }
        return;
      }
      const message = parseControlMessage(event.data);
      if (!message) return;
      if (message.type === 'ready') {
        reconnectAttempts = 0;
        handlers.onConnectionChange?.(true);
        handlers.onReady();
        if (latestSize) sendJSON({ type: 'resize', ...latestSize });
        return;
      }
      if (message.type === 'exit') {
        terminalEnded = true;
        handlers.onExit();
        return;
      }
      if (message.type === 'error') {
        terminalEnded = true;
        handlers.onError(message.message || 'Terminal 连接失败');
      }
    });
    current.addEventListener('close', () => {
      if (socket === current) socket = null;
      handlers.onConnectionChange?.(false);
      if (disconnected || terminalEnded) return;
      reconnectAttempts += 1;
      reconnectTimer = setTimeout(connect, Math.min(500 * 2 ** (reconnectAttempts - 1), 5000));
    });
    current.addEventListener('error', () => {
      current.close();
    });
  }

  function sendJSON(message: Record<string, unknown>) {
    if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify(message));
  }
}

function terminalWebSocketURL(sessionId: string) {
  const url = new URL(`/api/terminals/${encodeURIComponent(sessionId)}/ws`, window.location.href);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  return url.toString();
}

function bearerAuthorization() {
  const accessKey = getGraphQLAccessKey();
  return accessKey ? `Bearer ${accessKey}` : '';
}

function parseControlMessage(raw: string): { type: string; message?: string } | null {
  try {
    const value = JSON.parse(raw) as { type?: unknown; message?: unknown };
    if (typeof value.type !== 'string') return null;
    const result: { type: string; message?: string } = { type: value.type };
    if (typeof value.message === 'string') result.message = value.message;
    return result;
  } catch {
    return null;
  }
}
