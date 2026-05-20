import { createContext, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';

type WSCallback = (data: unknown) => void;

interface WSContextValue {
  connected: boolean;
  subscribe: (type: string, cb: WSCallback) => () => void;
}

export const WebSocketContext = createContext<WSContextValue>({
  connected: false,
  subscribe: () => () => {},
});

export function WebSocketProvider({ children }: { children: ReactNode }) {
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const subsRef = useRef<Map<string, Set<WSCallback>>>(new Map());
  const reconnectRef = useRef(0);

  const connect = useCallback(() => {
    let mounted = true;
    let pingInterval: ReturnType<typeof setInterval> | null = null;

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/ws/events`);
    wsRef.current = ws;

    ws.onopen = () => {
      if (!mounted) { ws.close(); return; }
      setConnected(true);
      reconnectRef.current = 0;

      pingInterval = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }));
        }
      }, 30000);
    };

    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        if (msg.type === 'pong') return;
        const cbs = subsRef.current.get(msg.type);
        if (cbs) cbs.forEach((cb) => cb(msg.payload));
      } catch {
        // ignore malformed messages
      }
    };

    ws.onerror = () => {
      // onclose will fire after onerror, triggering reconnect
      console.debug('[ws] connection error, will retry...');
    };

    ws.onclose = () => {
      if (pingInterval) clearInterval(pingInterval);
      if (!mounted) return;
      setConnected(false);
      const delay = Math.min(1000 * 2 ** reconnectRef.current, 30000);
      reconnectRef.current++;
      setTimeout(connect, delay);
    };

    return () => {
      mounted = false;
      if (pingInterval) clearInterval(pingInterval);
    };
  }, []);

  useEffect(() => {
    const cleanup = connect();
    return () => {
      cleanup();
      wsRef.current?.close();
    };
  }, [connect]);

  const subscribe = useCallback((type: string, cb: WSCallback) => {
    const cbs = subsRef.current.get(type) ?? new Set();
    cbs.add(cb);
    subsRef.current.set(type, cbs);
    return () => {
      cbs.delete(cb);
      if (cbs.size === 0) subsRef.current.delete(type);
    };
  }, []);

  return (
    <WebSocketContext.Provider value={{ connected, subscribe }}>
      {children}
    </WebSocketContext.Provider>
  );
}
