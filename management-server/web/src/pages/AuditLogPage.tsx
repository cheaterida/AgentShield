import { useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { useAuditEvents } from '../hooks/useAuditEvents';

export function AuditLogPage() {
  const [params, setParams] = useState('limit=50');
  const { events, total, loading, error, refresh } = useAuditEvents(params);
  const [action, setAction] = useState('');
  const [agentId, setAgentId] = useState('');

  const applyFilter = () => {
    const p = new URLSearchParams();
    p.set('limit', '50');
    if (action) p.set('action', action);
    if (agentId) p.set('agent_id', agentId);
    setParams(p.toString());
  };

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700 }}>审计日志</h1>
        <button
          onClick={refresh}
          style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 16px', borderRadius: 8, border: '1px solid #e2e8f0', background: '#fff', cursor: 'pointer', fontSize: 13 }}
        >
          <RefreshCw size={14} /> 刷新
        </button>
      </div>

      <div style={{ display: 'flex', gap: 12, marginBottom: 16 }}>
        <input
          placeholder="按操作过滤 (read/write/exec...)"
          value={action}
          onChange={(e) => setAction(e.target.value)}
          style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13, flex: 1 }}
        />
        <input
          placeholder="按 Agent ID 过滤"
          value={agentId}
          onChange={(e) => setAgentId(e.target.value)}
          style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13, flex: 1 }}
        />
        <button
          onClick={applyFilter}
          style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#6366f1', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}
        >
          搜索
        </button>
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
          <div style={{ padding: '10px 16px', background: '#f8fafc', fontSize: 12, color: '#64748b' }}>共 {total} 条记录</div>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0' }}>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>时间</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>Agent</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>操作</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>资源</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>风险贡献</th>
              </tr>
            </thead>
            <tbody>
              {events.length === 0 ? (
                <tr><td colSpan={5} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无事件</td></tr>
              ) : (
                events.map((ev) => (
                  <tr key={ev.event_id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '10px 16px', fontSize: 12, color: '#94a3b8' }}>{new Date(ev.occurred_at).toLocaleString()}</td>
                    <td style={{ padding: '10px 16px', color: '#6366f1' }}>{ev.agent_id}</td>
                    <td style={{ padding: '10px 16px', fontWeight: 500 }}>{ev.action}</td>
                    <td style={{ padding: '10px 16px', color: '#475569', maxWidth: 300, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{ev.resource_ref}</td>
                    <td style={{ padding: '10px 16px' }}>{(ev.risk_contribution ?? 0).toFixed(2)}</td>
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
