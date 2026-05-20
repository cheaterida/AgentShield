import { useCallback, useEffect, useState } from 'react';
import { api } from '../api/client';
import type { Agent } from '../api/types';

export function useAgents(status?: string) {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchAgents = useCallback(async () => {
    try {
      const params = status ? `status=${status}` : '';
      const data = await api.listAgents(params);
      setAgents(data.agents);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载智能体列表失败');
    } finally {
      setLoading(false);
    }
  }, [status]);

  useEffect(() => {
    fetchAgents();
    const t = setInterval(fetchAgents, 15000);
    return () => clearInterval(t);
  }, [fetchAgents]);

  return { agents, loading, error, refresh: fetchAgents };
}
