import { useCallback, useEffect, useState } from 'react';
import { BarChart3, DollarSign, Cpu, Coins } from 'lucide-react';
import {
  AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip,
  PieChart, Pie, Cell, BarChart, Bar,
  ResponsiveContainer, Legend,
} from 'recharts';
import { api } from '../api/client';

const cardStyle: React.CSSProperties = {
  background: '#fff',
  borderRadius: 12,
  padding: '20px 24px',
  boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
  display: 'flex',
  alignItems: 'center',
  gap: 16,
};

const PIE_COLORS = ['#6366f1', '#16a34a', '#eab308', '#ef4444', '#f97316', '#a855f7', '#06b6d4', '#64748b'];

export function TokenUsagePage() {
  const [dailySummary, setDailySummary] = useState<{ date_key: string; total_tokens: number }[]>([]);
  const [modelPie, setModelPie] = useState<{ name: string; value: number }[]>([]);
  const [agentRank, setAgentRank] = useState<{ name: string; tokens: number }[]>([]);
  const [stats, setStats] = useState({ todayTokens: 0, monthlyCost: 0, modelCount: 0 });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const fetchData = useCallback(async () => {
    try {
      setError(null);
      const [pricesRes, usageLogsRes] = await Promise.all([
        api.listPrices(),
        api.listUsageLogs('limit=100'),
      ]);

      const prices = pricesRes.prices;
      const logs = usageLogsRes.logs;

      const dailyMap = new Map<string, number>();
      const modelMap = new Map<string, number>();
      const agentMap = new Map<string, number>();
      let todayTotal = 0;
      const today = new Date().toISOString().slice(0, 10);

      for (const log of logs) {
        const day = log.occurred_at.slice(0, 10);
        dailyMap.set(day, (dailyMap.get(day) || 0) + log.total_tokens);
        modelMap.set(log.model_name, (modelMap.get(log.model_name) || 0) + log.total_tokens);
        agentMap.set(log.agent_id, (agentMap.get(log.agent_id) || 0) + log.total_tokens);
        if (day === today) todayTotal += log.total_tokens;
      }

      const dailySummary = [...dailyMap.entries()]
        .map(([date_key, total_tokens]) => ({ date_key, total_tokens }))
        .sort((a, b) => a.date_key.localeCompare(b.date_key));

      const modelSorted = [...modelMap.entries()].sort((a, b) => b[1] - a[1]);
      const modelPie = modelSorted.slice(0, 7).map(([name, value]) => ({ name, value }));
      const otherValue = modelSorted.slice(7).reduce((s, [, v]) => s + v, 0);
      if (otherValue > 0) modelPie.push({ name: 'Other', value: otherValue });

      const agentRank = [...agentMap.entries()]
        .sort((a, b) => b[1] - a[1])
        .slice(0, 10)
        .map(([name, tokens]) => ({ name, tokens }));

      setDailySummary(dailySummary.length > 0 ? dailySummary : []);
      setModelPie(modelPie);
      setAgentRank(agentRank);
      setStats({
        todayTokens: todayTotal,
        monthlyCost: logs.reduce((s, l) => s + l.cost_millicents, 0),
        modelCount: prices.length,
      });
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load data');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchData();
    const t = setInterval(fetchData, 15000);
    return () => clearInterval(t);
  }, [fetchData]);

  if (error) {
    return (
      <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
        <p style={{ fontWeight: 600, marginBottom: 8 }}>Failed to load</p>
        <p style={{ fontSize: 13 }}>{error}</p>
        <button onClick={fetchData} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>Retry</button>
      </div>
    );
  }

  if (loading) {
    return <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>Loading...</div>;
  }

  const hasData = dailySummary.length > 0 || modelPie.length > 0;

  if (!hasData) {
    return (
      <div>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>Token Usage</h1>
        <div style={cardStyle}>
          <Coins size={48} color="#94a3b8" />
          <div>
            <div style={{ fontSize: 16, fontWeight: 600, color: '#334155', marginBottom: 4 }}>No token usage data yet</div>
            <div style={{ color: '#94a3b8', fontSize: 13 }}>Usage data will appear here once agents start calling LLM APIs.</div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div>
      <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>Token Usage</h1>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 16, marginBottom: 24 }}>
        <div style={cardStyle}>
          <BarChart3 size={32} color="#6366f1" />
          <div>
            <div style={{ fontSize: 28, fontWeight: 700 }}>{stats.todayTokens.toLocaleString()}</div>
            <div style={{ color: '#64748b', fontSize: 13 }}>Today's Token Usage</div>
          </div>
        </div>
        <div style={cardStyle}>
          <DollarSign size={32} color="#16a34a" />
          <div>
            <div style={{ fontSize: 28, fontWeight: 700 }}>${(stats.monthlyCost / 100000).toFixed(2)}</div>
            <div style={{ color: '#64748b', fontSize: 13 }}>Estimated Cost (This Month)</div>
          </div>
        </div>
        <div style={cardStyle}>
          <Cpu size={32} color="#f59e0b" />
          <div>
            <div style={{ fontSize: 28, fontWeight: 700 }}>{stats.modelCount}</div>
            <div style={{ color: '#64748b', fontSize: 13 }}>Model Types</div>
          </div>
        </div>
      </div>

      <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch', marginBottom: 24 }}>
        <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>Token Usage Trend (Daily)</div>
        <ResponsiveContainer width="100%" height={250}>
          <AreaChart data={dailySummary}>
            <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
            <XAxis dataKey="date_key" fontSize={12} />
            <YAxis fontSize={12} />
            <Tooltip />
            <Area type="monotone" dataKey="total_tokens" stroke="#6366f1" fill="#6366f120" name="Tokens" />
          </AreaChart>
        </ResponsiveContainer>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 24 }}>
        <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch' }}>
          <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>Model Distribution</div>
          {modelPie.length > 0 ? (
            <ResponsiveContainer width="100%" height={250}>
              <PieChart>
                <Pie data={modelPie} cx="50%" cy="50%" outerRadius={80} dataKey="value" label={({ name, percent }) => `${name} ${(percent * 100).toFixed(0)}%`}>
                  {modelPie.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
                </Pie>
                <Tooltip />
              </PieChart>
            </ResponsiveContainer>
          ) : (
            <div style={{ color: '#94a3b8', fontSize: 14, padding: 40, textAlign: 'center' }}>No data available</div>
          )}
        </div>

        <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch' }}>
          <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>Agent Ranking</div>
          {agentRank.length > 0 ? (
            <ResponsiveContainer width="100%" height={250}>
              <BarChart data={agentRank} layout="vertical">
                <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
                <XAxis type="number" fontSize={12} />
                <YAxis type="category" dataKey="name" width={90} fontSize={12} />
                <Tooltip />
                <Bar dataKey="tokens" fill="#6366f1" radius={[0, 4, 4, 0]} />
              </BarChart>
            </ResponsiveContainer>
          ) : (
            <div style={{ color: '#94a3b8', fontSize: 14, padding: 40, textAlign: 'center' }}>No data available</div>
          )}
        </div>
      </div>
    </div>
  );
}
