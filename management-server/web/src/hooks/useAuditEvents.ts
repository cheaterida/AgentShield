import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { AuditEvent } from '../api/types';

export function useAuditEvents(params?: string) {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  const fetchEvents = useCallback(async () => {
    try {
      const data = await api.listAuditEvents(params);
      setEvents(data.events);
      setTotal(data.total);
    } catch (err) {
      console.error('fetch audit events:', err);
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => {
    fetchEvents();
    const t = setInterval(fetchEvents, 10000);
    return () => clearInterval(t);
  }, [fetchEvents]);

  return { events, total, loading, refresh: fetchEvents };
}
