import { useCallback, useEffect, useState } from 'react';
import { Bot, AlertTriangle, BarChart3, Wifi } from 'lucide-react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, PieChart, Pie, Cell, ResponsiveContainer } from 'recharts';
import { api } from '../api/client';
import type { DashboardStats } from '../api/types';
import { useWebSocket } from '../hooks/useWebSocket';
import { SeverityBadge } from '../components/common/SeverityBadge';

const cardStyle: React.CSSProperties = {
  background: '#fff',
  borderRadius: 12,
  padding: '20px 24px',
  boxShadow: '0 1px 3px rgba(0,0,0,0.06)',
  display: 'flex',
  alignItems: 'center',
  gap: 16,
};

export function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const wsEvent = useWebSocket('audit_event');

  const fetchStats = useCallback(async (silent?: boolean) => {
    try {
      setStats(await api.getDashboardStats());
      setError(null);
    } catch (e) {
      if (!silent) setError(e instanceof Error ? e.message : '加载仪表盘数据失败');
    }
  }, []);

  useEffect(() => {
    fetchStats();
    const t = setInterval(() => fetchStats(true), 10000);
    return () => clearInterval(t);
  }, [fetchStats]);

  // Refresh on WS event
  useEffect(() => {
    if (wsEvent) fetchStats(true);
  }, [wsEvent, fetchStats]);

  if (error) {
    return (
      <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
        <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
        <p style={{ fontSize: 13 }}>{error}</p>
        <button onClick={() => fetchStats()} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
      </div>
    );
  }

  if (!stats) {
    return <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>;
  }

  const pieData = [
    { name: '在线', value: stats.online_agent_count, color: '#16a34a' },
    { name: '可疑', value: stats.suspicious_agent_count, color: '#eab308' },
    { name: '其他', value: Math.max(0, stats.agent_count - stats.online_agent_count - stats.suspicious_agent_count), color: '#9ca3af' },
  ];

  return (
    <div>
      <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>仪表盘</h1>

      {/* Stat cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 16, marginBottom: 24 }}>
        <div style={cardStyle}>
          <Bot size={32} color="#6366f1" />
          <div><div style={{ fontSize: 28, fontWeight: 700 }}>{stats.agent_count}</div><div style={{ color: '#64748b', fontSize: 13 }}>智能体总数</div></div>
        </div>
        <div style={cardStyle}>
          <Wifi size={32} color="#16a34a" />
          <div><div style={{ fontSize: 28, fontWeight: 700 }}>{stats.online_agent_count}</div><div style={{ color: '#64748b', fontSize: 13 }}>在线</div></div>
        </div>
        <div style={cardStyle}>
          <AlertTriangle size={32} color="#ef4444" />
          <div><div style={{ fontSize: 28, fontWeight: 700 }}>{stats.open_alert_count}</div><div style={{ color: '#64748b', fontSize: 13 }}>未解决告警</div></div>
        </div>
        <div style={cardStyle}>
          <BarChart3 size={32} color="#f59e0b" />
          <div><div style={{ fontSize: 28, fontWeight: 700 }}>{stats.event_rate_last_hour}</div><div style={{ color: '#64748b', fontSize: 13 }}>事件/分钟 (近1h)</div></div>
        </div>
      </div>

      {/* Charts */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 300px', gap: 16, marginBottom: 24 }}>
        <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch' }}>
          <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>事件速率趋势</div>
          <ResponsiveContainer width="100%" height={200}>
            <AreaChart data={[{ time: '1h前', events: Math.round(stats.event_rate_last_hour * 0.8) }, { time: '30min前', events: Math.round(stats.event_rate_last_hour * 0.9) }, { time: '现在', events: stats.event_rate_last_hour }]}>
              <CartesianGrid strokeDasharray="3 3" stroke="#e2e8f0" />
              <XAxis dataKey="time" fontSize={12} />
              <YAxis fontSize={12} />
              <Tooltip />
              <Area type="monotone" dataKey="events" stroke="#6366f1" fill="#6366f120" />
            </AreaChart>
          </ResponsiveContainer>
        </div>
        <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch' }}>
          <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>状态分布</div>
          <ResponsiveContainer width="100%" height={200}>
            <PieChart>
              <Pie data={pieData} cx="50%" cy="50%" innerRadius={50} outerRadius={80} dataKey="value">
                {pieData.map((d, i) => <Cell key={i} fill={d.color} />)}
              </Pie>
              <Tooltip />
            </PieChart>
          </ResponsiveContainer>
        </div>
      </div>

      {/* Recent alerts */}
      <div style={{ ...cardStyle, flexDirection: 'column', alignItems: 'stretch' }}>
        <div style={{ fontWeight: 600, marginBottom: 12, color: '#334155' }}>最近告警</div>
        {stats.recent_alerts.length === 0 ? (
          <div style={{ color: '#94a3b8', fontSize: 14, padding: 16, textAlign: 'center' }}>暂无告警</div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0' }}>
                <th style={{ textAlign: 'left', padding: '8px 0', color: '#64748b' }}>级别</th>
                <th style={{ textAlign: 'left', padding: '8px 0', color: '#64748b' }}>标题</th>
                <th style={{ textAlign: 'left', padding: '8px 0', color: '#64748b' }}>智能体</th>
                <th style={{ textAlign: 'left', padding: '8px 0', color: '#64748b' }}>时间</th>
              </tr>
            </thead>
            <tbody>
              {stats.recent_alerts.map((a) => (
                <tr key={a.alert_id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                  <td style={{ padding: '8px 0' }}><SeverityBadge severity={a.severity} /></td>
                  <td style={{ padding: '8px 0' }}>{a.title}</td>
                  <td style={{ padding: '8px 0', color: '#6366f1' }}>{a.agent_id}</td>
                  <td style={{ padding: '8px 0', color: '#94a3b8', fontSize: 12 }}>{new Date(a.occurred_at).toLocaleString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
