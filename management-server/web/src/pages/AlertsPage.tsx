import { useState } from 'react';
import { CheckCircle, XCircle } from 'lucide-react';
import { useAlerts } from '../hooks/useAlerts';
import { SeverityBadge } from '../components/common/SeverityBadge';
import { api } from '../api/client';

const SEVERITIES = ['', 'low', 'medium', 'high', 'critical'];
const STATUSES = ['', 'open', 'acknowledged', 'resolved', 'dismissed'];

export function AlertsPage() {
  const [severity, setSeverity] = useState('');
  const [status, setStatus] = useState('');
  const params = [severity && `severity=${severity}`, status && `status=${status}`].filter(Boolean).join('&');
  const { alerts, total, loading, refresh } = useAlerts(params || undefined);

  const handleStatusChange = async (alertId: string, newStatus: string) => {
    try {
      await api.updateAlert(alertId, newStatus);
      refresh();
    } catch (e) {
      console.error(e);
    }
  };

  return (
    <div>
      <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>安全告警</h1>

      <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
        <span style={{ fontSize: 13, color: '#64748b', alignSelf: 'center' }}>级别:</span>
        {SEVERITIES.map((s) => (
          <button key={s || 'all'} onClick={() => setSeverity(s)}
            style={{ padding: '4px 12px', borderRadius: 9999, border: '1px solid #e2e8f0', background: severity === s ? '#6366f1' : '#fff', color: severity === s ? '#fff' : '#475569', fontSize: 12, cursor: 'pointer' }}>
            {s || '全部'}
          </button>
        ))}
        <span style={{ fontSize: 13, color: '#64748b', alignSelf: 'center', marginLeft: 12 }}>状态:</span>
        {STATUSES.map((s) => (
          <button key={s || 'all'} onClick={() => setStatus(s)}
            style={{ padding: '4px 12px', borderRadius: 9999, border: '1px solid #e2e8f0', background: status === s ? '#6366f1' : '#fff', color: status === s ? '#fff' : '#475569', fontSize: 12, cursor: 'pointer' }}>
            {s || '全部'}
          </button>
        ))}
      </div>

      {loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      ) : (
        <div style={{ background: '#fff', borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' }}>
          <div style={{ padding: '10px 16px', background: '#f8fafc', fontSize: 12, color: '#64748b' }}>共 {total} 条告警</div>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0' }}>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>级别</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>标题</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>描述</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>Agent</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>状态</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>时间</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600 }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {alerts.length === 0 ? (
                <tr><td colSpan={7} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无告警</td></tr>
              ) : (
                alerts.map((a) => (
                  <tr key={a.alert_id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                    <td style={{ padding: '10px 16px' }}><SeverityBadge severity={a.severity} /></td>
                    <td style={{ padding: '10px 16px', fontWeight: 500 }}>{a.title}</td>
                    <td style={{ padding: '10px 16px', color: '#64748b', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{a.description}</td>
                    <td style={{ padding: '10px 16px', color: '#6366f1' }}>{a.agent_id}</td>
                    <td style={{ padding: '10px 16px' }}><SeverityBadge severity={a.status} /></td>
                    <td style={{ padding: '10px 16px', fontSize: 12, color: '#94a3b8' }}>{new Date(a.occurred_at).toLocaleString()}</td>
                    <td style={{ padding: '10px 16px' }}>
                      {a.status === 'open' && (
                        <div style={{ display: 'flex', gap: 4 }}>
                          <button onClick={() => handleStatusChange(a.alert_id, 'acknowledged')} title="确认" style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#f59e0b' }}><CheckCircle size={16} /></button>
                          <button onClick={() => handleStatusChange(a.alert_id, 'dismissed')} title="忽略" style={{ border: 'none', background: 'none', cursor: 'pointer', color: '#94a3b8' }}><XCircle size={16} /></button>
                        </div>
                      )}
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
