import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { AuditEvent } from '../api/types';

export function useAuditEvents(params?: string) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchEvents = useCallback(async () => {
    try {
      const data = await api.listAuditEvents(params);
      console.debug('[useAuditEvents] fetch success — events:', Array.isArray(data?.events) ? data.events.length : typeof data?.events, 'total:', data?.total);
      setEvents(data.events || []);
      setTotal(data.total ?? 0);
      setError(null);
    } catch (err) {
      console.error('[useAuditEvents] fetch error:', err);
      setError(err instanceof Error ? err.message : '加载审计事件失败');
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => {
    fetchEvents();
    const t = setInterval(fetchEvents, 10000);
    return () => clearInterval(t);
  }, [fetchEvents]);

  return { events, total, loading, error, refresh: fetchEvents };
}
