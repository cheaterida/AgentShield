import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useAgents } from '../hooks/useAgents';
import { StatusBadge } from '../components/common/StatusBadge';

const FILTERS = ['', 'online', 'offline', 'suspicious', 'degraded'];

export function AgentsPage() {
  const [statusFilter, setStatusFilter] = useState('');
  const { agents, loading, error, refresh } = useAgents(statusFilter || undefined);

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700 }}>智能体管理</h1>
      </div>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        {FILTERS.map((f) => (
          <button
            key={f || 'all'}
            onClick={() => setStatusFilter(f)}
            style={{
              padding: '6px 14px',
              borderRadius: 9999,
              border: '1px solid #e2e8f0',
              background: statusFilter === f ? '#6366f1' : '#fff',
              color: statusFilter === f ? '#fff' : '#475569',
              fontSize: 13,
              fontWeight: 500,
              cursor: 'pointer',
            }}
          >
            {f || '全部'}
          </button>
        ))}
      </div>

      {error ? (
        <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
          <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
          <p style={{ fontSize: 13 }}>{error}</p>
          <button onClick={refresh} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
        </div>
      ) : loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      ) : (
        <div style={{ background: '#fff', borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0', background: '#f8fafc' }}>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>ID</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>名称</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>家庭组</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>状态</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>风险评分</th>
                <th style={{ textAlign: 'left', padding: '12px 16px', color: '#64748b', fontWeight: 600 }}>最后心跳</th>
              </tr>
            </thead>
            <tbody>
              {agents.length === 0 ? (
                <tr><td colSpan={6} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无注册智能体</td></tr>
              ) : (
                agents.map((a) => (
                  <tr key={a.id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '12px 16px' }}>
                      <Link to={`/agents/${a.id}`} style={{ color: '#6366f1', textDecoration: 'none', fontWeight: 500 }}>
                        {a.id}
                      </Link>
                    </td>
                    <td style={{ padding: '12px 16px' }}>{a.display_name || '-'}</td>
                    <td style={{ padding: '12px 16px', color: '#475569' }}>{a.family_group_id}</td>
                    <td style={{ padding: '12px 16px' }}><StatusBadge status={a.status} /></td>
                    <td style={{ padding: '12px 16px' }}>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <div style={{ flex: 1, maxWidth: 80, height: 6, borderRadius: 3, background: '#e2e8f0' }}>
                          <div style={{ height: '100%', borderRadius: 3, background: a.risk_score > 0.6 ? '#ef4444' : a.risk_score > 0.3 ? '#f59e0b' : '#16a34a', width: `${(a.risk_score * 100).toFixed(0)}%` }} />
                        </div>
                        <span style={{ fontSize: 12, color: '#64748b' }}>{(a.risk_score * 100).toFixed(0)}%</span>
                      </div>
                    </td>
                    <td style={{ padding: '12px 16px', color: '#94a3b8', fontSize: 12 }}>
                      {a.last_heartbeat_at ? new Date(a.last_heartbeat_at).toLocaleString() : '从未'}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
