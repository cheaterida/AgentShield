import { useCallback, useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { api } from '../api/client';
import { StatusBadge } from '../components/common/StatusBadge';
import type { Agent, AuditEvent } from '../api/types';

export function AgentDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [agent, setAgent] = useState<Agent | null>(null);
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [tokenUsage, setTokenUsage] = useState<Record<string, number> | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchAgent = useCallback(async (silent?: boolean) => {
    if (!id) return;
    try {
      setAgent(await api.getAgent(id));
      setError(null);
    } catch (e) {
      if (!silent) setError(e instanceof Error ? e.message : '加载智能体详情失败');
    }
  }, [id]);

  const fetchEvents = useCallback(async (silent?: boolean) => {
    if (!id) return;
    try {
      const data = await api.listAuditEvents(`agent_id=${id}&limit=20`);
      setEvents(data.events);
      setError(null);
    } catch (e) {
      if (!silent) setError(e instanceof Error ? e.message : '加载审计事件失败');
    }
  }, [id]);

  const fetchTokenUsage = useCallback(async (silent?: boolean) => {
    if (!id) return;
    try {
      setTokenUsage(await api.getAgentUsage(id));
    } catch (e) {
      if (!silent) console.error('failed to fetch token usage', e);
    }
  }, [id]);

  useEffect(() => {
    fetchAgent();
    fetchEvents();
    fetchTokenUsage();
    const t = setInterval(() => { fetchAgent(true); fetchEvents(true); fetchTokenUsage(true); }, 10000);
    return () => clearInterval(t);
  }, [fetchAgent, fetchEvents, fetchTokenUsage]);

  if (error) {
    return (
      <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
        <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
        <p style={{ fontSize: 13 }}>{error}</p>
        <button onClick={() => { fetchAgent(); fetchEvents(); }} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
      </div>
    );
  }

  if (!agent) {
    return <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>;
  }

  const cardStyle = { background: '#fff', borderRadius: 12, padding: 24, boxShadow: '0 1px 3px rgba(0,0,0,0.06)' };

  return (
    <div>
      <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24 }}>智能体详情</h1>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 24 }}>
        <div style={cardStyle}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>基本信息</h3>
          <dl style={{ display: 'grid', gridTemplateColumns: '100px 1fr', gap: '8px 16px', fontSize: 14 }}>
            <dt style={{ color: '#64748b' }}>ID</dt><dd style={{ fontWeight: 500 }}>{agent.id}</dd>
            <dt style={{ color: '#64748b' }}>名称</dt><dd>{agent.display_name || '-'}</dd>
            <dt style={{ color: '#64748b' }}>家庭组</dt><dd>{agent.family_group_id}</dd>
            <dt style={{ color: '#64748b' }}>状态</dt><dd><StatusBadge status={agent.status} /></dd>
            <dt style={{ color: '#64748b' }}>注册时间</dt><dd style={{ fontSize: 12, color: '#94a3b8' }}>{new Date(agent.registered_at).toLocaleString()}</dd>
            <dt style={{ color: '#64748b' }}>最后心跳</dt><dd style={{ fontSize: 12, color: '#94a3b8' }}>{agent.last_heartbeat_at ? new Date(agent.last_heartbeat_at).toLocaleString() : '从未'}</dd>
          </dl>
        </div>

        <div style={cardStyle}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>风险评分</h3>
          <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
            <div style={{ position: 'relative', width: 120, height: 120 }}>
              <svg viewBox="0 0 120 120" style={{ transform: 'rotate(-90deg)' }}>
                <circle cx={60} cy={60} r={50} fill="none" stroke="#e2e8f0" strokeWidth={10} />
                <circle cx={60} cy={60} r={50} fill="none" stroke={agent.risk_score > 0.6 ? '#ef4444' : agent.risk_score > 0.3 ? '#f59e0b' : '#16a34a'} strokeWidth={10} strokeDasharray={`${agent.risk_score * 314} 314`} strokeLinecap="round" />
              </svg>
              <div style={{ position: 'absolute', inset: 0, display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: 24, fontWeight: 700 }}>
                {(agent.risk_score * 100).toFixed(0)}%
              </div>
            </div>
            <div>
              <div style={{ fontSize: 14, color: '#64748b' }}>评分范围: 0-100%</div>
              <div style={{ fontSize: 13, color: '#94a3b8', marginTop: 4 }}>&lt;30% 正常 | 30-60% 关注 | &gt;60% 危险</div>
            </div>
          </div>
        </div>
      </div>

      {/* Token Usage */}
      {tokenUsage && (
        <div style={{ ...cardStyle, marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>Token 用量</h3>
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16 }}>
            <div style={{ background: '#f8fafc', borderRadius: 8, padding: 12 }}>
              <div style={{ color: '#64748b', fontSize: 12, marginBottom: 4 }}>今日</div>
              <div style={{ fontSize: 20, fontWeight: 700, color: '#334155' }}>{(tokenUsage.daily_total_tokens || 0).toLocaleString()}</div>
              <div style={{ color: '#94a3b8', fontSize: 11 }}>${((tokenUsage.daily_cost_millicents || 0) / 100000).toFixed(2)}</div>
            </div>
            <div style={{ background: '#f8fafc', borderRadius: 8, padding: 12 }}>
              <div style={{ color: '#64748b', fontSize: 12, marginBottom: 4 }}>本月</div>
              <div style={{ fontSize: 20, fontWeight: 700, color: '#334155' }}>{(tokenUsage.monthly_total_tokens || 0).toLocaleString()}</div>
              <div style={{ color: '#94a3b8', fontSize: 11 }}>${((tokenUsage.monthly_cost_millicents || 0) / 100000).toFixed(2)}</div>
            </div>
            <div style={{ background: '#f8fafc', borderRadius: 8, padding: 12 }}>
              <div style={{ color: '#64748b', fontSize: 12, marginBottom: 4 }}>累计</div>
              <div style={{ fontSize: 20, fontWeight: 700, color: '#334155' }}>{(tokenUsage.total_total_tokens || 0).toLocaleString()}</div>
              <div style={{ color: '#94a3b8', fontSize: 11 }}>${((tokenUsage.total_cost_millicents || 0) / 100000).toFixed(2)}</div>
            </div>
          </div>
        </div>
      )}

      {/* Event timeline */}
      <div style={cardStyle}>
        <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>最近审计事件</h3>
        {events.length === 0 ? (
          <div style={{ color: '#94a3b8', textAlign: 'center', padding: 24 }}>暂无事件</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {events.map((ev) => (
              <div key={ev.event_id} style={{ display: 'flex', gap: 12, padding: '8px 12px', borderRadius: 8, background: '#f8fafc', fontSize: 13 }}>
                <span style={{ color: '#94a3b8', fontSize: 12, minWidth: 140 }}>{new Date(ev.occurred_at).toLocaleString()}</span>
                <span style={{ fontWeight: 500, color: '#ef4444', minWidth: 60 }}>{ev.action}</span>
                <span style={{ color: '#475569', flex: 1 }}>{ev.resource_ref}</span>
                <span style={{ color: '#94a3b8' }}>风险: {(ev.risk_contribution * 100).toFixed(0)}%</span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
