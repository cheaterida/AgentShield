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
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${proto}//${location.host}/api/v1/ws/events`);
    wsRef.current = ws;

    ws.onopen = () => {
      setConnected(true);
      reconnectRef.current = 0;
    };

    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data);
        const cbs = subsRef.current.get(msg.type);
        if (cbs) cbs.forEach((cb) => cb(msg.payload));
      } catch {
        // ignore malformed messages
      }
    };

    ws.onclose = () => {
      setConnected(false);
      const delay = Math.min(1000 * 2 ** reconnectRef.current, 30000);
      reconnectRef.current++;
      setTimeout(connect, delay);
    };
  }, []);

  useEffect(() => {
    connect();
    return () => wsRef.current?.close();
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
