import { useContext, useEffect, useState } from 'react';
import { WebSocketContext } from '../context/WebSocketContext';

export function useWebSocket<T = unknown>(type: string) {
  const { subscribe } = useContext(WebSocketContext);
  const [last, setLast] = useState<T | null>(null);

  useEffect(() => {
    return subscribe(type, (data) => setLast(data as T));
  }, [subscribe, type]);

  return last;
}
