import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { RiskAlert } from '../api/types';

export function useAlerts(params?: string) {
  const [alerts, setAlerts] = useState<RiskAlert[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchAlerts = useCallback(async () => {
    try {
      const data = await api.listAlerts(params);
      setAlerts(data.alerts);
      setTotal(data.total);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载告警列表失败');
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => {
    fetchAlerts();
    const t = setInterval(fetchAlerts, 15000);
    return () => clearInterval(t);
  }, [fetchAlerts]);

  return { alerts, total, loading, error, refresh: fetchAlerts };
}
