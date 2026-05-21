import { useState, useEffect, useCallback, useRef } from 'react';
import { RefreshCw, Shield, ShieldAlert, FileText, Terminal, Wifi, Server } from 'lucide-react';
import { api } from '../api/client';
import type { AuditEvent } from '../api/types';

const SYSCALL_META: Record<string, { label: string; icon: typeof Shield; color: string; bg: string }> = {
  openat: { label: '文件访问', icon: FileText, color: '#2563eb', bg: '#dbeafe' },
  execve: { label: '进程执行', icon: Terminal, color: '#16a34a', bg: '#d1fae5' },
  connect: { label: '网络连接', icon: Wifi, color: '#f59e0b', bg: '#fef3c7' },
  bind: { label: '端口绑定', icon: Server, color: '#7c3aed', bg: '#ede9fe' },
};

function detectSyscall(event: AuditEvent): string {
  const action = event.action || '';
  if (action === 'network_connect' || action === 'socket_create') return action === 'network_connect' ? 'connect' : 'bind';
  if (action === 'exec') return 'execve';
  if (action === 'read' || action === 'write') return 'openat';
  return action;
}

const SYSCALL_TO_ACTION: Record<string, string> = {
  connect: 'network_connect',
  bind: 'socket_create',
  execve: 'exec',
  openat: 'read',  // 'read' covers the most common file access case
};

function formatTime(iso: string): string {
  try { return new Date(iso).toLocaleString(); } catch { return iso; }
}

const severityBadge: Record<string, React.CSSProperties> = {
  low: { background: '#d1fae5', color: '#047857' },
  medium: { background: '#fef3c7', color: '#b45309' },
  high: { background: '#fee2e2', color: '#dc2626' },
  critical: { background: '#fce7f3', color: '#be185d' },
};

export function SecurityEventsPage() {
  const [events, setEvents] = useState<AuditEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [filter, setFilter] = useState('');
  const [params, setParams] = useState('limit=50');
  const initialLoadDone = useRef(false);

  const fetchEvents = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.listAuditEvents(params);
      console.debug('[SecurityEvents] fetch success — events:', Array.isArray(data?.events) ? data.events.length : typeof data?.events, 'total:', data?.total);
      setEvents(data.events || []);
      setTotal(data.total ?? 0);
      setError(null);
      initialLoadDone.current = true;
    } catch (err) {
      console.error('[SecurityEvents] fetch error:', err);
      if (!initialLoadDone.current) {
        setError('加载安全事件失败');
      }
    } finally {
      setLoading(false);
    }
  }, [params]);

  useEffect(() => { fetchEvents(); }, [fetchEvents]);
  useEffect(() => {
    const interval = setInterval(fetchEvents, 10000);
    return () => clearInterval(interval);
  }, [fetchEvents]);

  const applyFilter = () => {
    const p = new URLSearchParams();
    p.set('limit', '50');
    if (filter) p.set('action', SYSCALL_TO_ACTION[filter] || filter);
    setParams(p.toString());
  };

  const syscallCounts = (events || []).reduce<Record<string, number>>((acc, ev) => {
    const s = detectSyscall(ev);
    acc[s] = (acc[s] || 0) + 1;
    return acc;
  }, {});

  const allSyscalls = Object.keys(SYSCALL_META);

  return (
    <div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 24 }}>
        <div>
          <h1 style={{ fontSize: 24, fontWeight: 700 }}>安全事件</h1>
          <p style={{ fontSize: 13, color: '#64748b', marginTop: 4 }}>eBPF 内核探针捕获的系统调用事件</p>
        </div>
        <button
          onClick={fetchEvents}
          style={{ display: 'flex', alignItems: 'center', gap: 6, padding: '8px 16px', borderRadius: 8, border: '1px solid #e2e8f0', background: '#fff', cursor: 'pointer', fontSize: 13 }}
        >
          <RefreshCw size={14} /> 刷新
        </button>
      </div>

      {/* Syscall type stat cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 12, marginBottom: 20 }}>
        {allSyscalls.map((s) => {
          const meta = SYSCALL_META[s];
          const Icon = meta.icon;
          const count = syscallCounts[s] || 0;
          return (
            <div
              key={s}
              onClick={() => { setFilter(s); const p = new URLSearchParams(); p.set('limit', '50'); p.set('action', SYSCALL_TO_ACTION[s] || s); setParams(p.toString()); }}
              style={{
                background: '#fff', borderRadius: 10, padding: '14px 16px', cursor: 'pointer',
                border: filter === s ? `2px solid ${meta.color}` : '1px solid #e2e8f0',
                boxShadow: '0 1px 3px rgba(0,0,0,0.05)', transition: 'all 0.15s',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8 }}>
                <div style={{ width: 32, height: 32, borderRadius: 8, background: meta.bg, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
                  <Icon size={16} color={meta.color} />
                </div>
                <span style={{ fontWeight: 600, fontSize: 14 }}>{meta.label}</span>
              </div>
              <div style={{ fontSize: 24, fontWeight: 700, color: meta.color }}>{count}</div>
              <div style={{ fontSize: 11, color: '#94a3b8' }}>{s}</div>
            </div>
          );
        })}
      </div>

      {/* Filter bar */}
      <div style={{ display: 'flex', gap: 12, marginBottom: 16 }}>
        <select
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          style={{ padding: '8px 12px', borderRadius: 8, border: '1px solid #e2e8f0', fontSize: 13, background: '#fff', minWidth: 150 }}
        >
          <option value="">全部事件类型</option>
          {allSyscalls.map((s) => (
            <option key={s} value={s}>{SYSCALL_META[s].label} ({s})</option>
          ))}
        </select>
        <button
          onClick={applyFilter}
          style={{ padding: '8px 20px', borderRadius: 8, border: 'none', background: '#6366f1', color: '#fff', fontSize: 13, fontWeight: 500, cursor: 'pointer' }}
        >
          筛选
        </button>
        {filter && (
          <button
            onClick={() => { setFilter(''); setParams('limit=50'); }}
            style={{ padding: '8px 16px', borderRadius: 8, border: '1px solid #e2e8f0', background: '#fff', fontSize: 13, cursor: 'pointer', color: '#64748b' }}
          >
            清除筛选
          </button>
        )}
      </div>

      {/* Events table */}
      {error ? (
        <div style={{ padding: 40, textAlign: 'center', background: '#fef2f2', borderRadius: 8, color: '#dc2626' }}>
          <p style={{ fontWeight: 600, marginBottom: 8 }}>加载失败</p>
          <p style={{ fontSize: 13 }}>{error}</p>
          <button onClick={fetchEvents} style={{ marginTop: 12, padding: '8px 16px', borderRadius: 8, border: '1px solid #fecaca', background: '#fff', color: '#dc2626', cursor: 'pointer', fontSize: 13 }}>重试</button>
        </div>
      ) : loading ? (
        <div style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>加载中...</div>
      ) : (
        <div style={{ background: '#fff', borderRadius: 12, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' }}>
          <div style={{ padding: '10px 16px', background: '#f8fafc', fontSize: 12, color: '#64748b', display: 'flex', justifyContent: 'space-between' }}>
            <span>共 {total} 条安全事件</span>
            <span>自动刷新 · 10s</span>
          </div>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ borderBottom: '1px solid #e2e8f0' }}>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 170 }}>时间</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 80 }}>类型</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 140 }}>目标资源</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 120 }}>进程</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 70 }}>PID</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 80 }}>风险分</th>
                <th style={{ textAlign: 'left', padding: '10px 16px', color: '#64748b', fontWeight: 600, width: 80 }}>OPA</th>
              </tr>
            </thead>
            <tbody>
              {(events || []).length === 0 ? (
                <tr><td colSpan={7} style={{ padding: 40, textAlign: 'center', color: '#94a3b8' }}>暂无安全事件</td></tr>
              ) : (
                (events || []).map((ev) => {
                  const syscall = detectSyscall(ev);
                  const meta = SYSCALL_META[syscall] || { label: syscall, color: '#64748b', bg: '#f1f5f9' };
                  const Icon = meta.icon || Shield;
                  const riskLevel = ev.attributes?.opa_risk_level || '';
                  const opaAllow = ev.attributes?.opa_allow;
                  const comm = ev.attributes?.comm || '-';
                  const pid = ev.attributes?.pid || '-';
                  const uid = ev.attributes?.uid || '-';

                  return (
                    <tr key={ev.event_id} style={{ borderBottom: '1px solid #f1f5f9' }}>
                      <td style={{ padding: '10px 16px', fontSize: 12, color: '#94a3b8' }}>{formatTime(ev.occurred_at)}</td>
                      <td style={{ padding: '10px 16px' }}>
                        <span style={{
                          display: 'inline-flex', alignItems: 'center', gap: 4, padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                          background: meta.bg, color: meta.color,
                        }}>
                          <Icon size={12} /> {meta.label}
                        </span>
                      </td>
                      <td style={{ padding: '10px 16px', color: '#475569', maxWidth: 200, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={ev.resource_ref}>
                        {ev.resource_ref}
                      </td>
                      <td style={{ padding: '10px 16px', fontFamily: "'SF Mono','Consolas',monospace", fontSize: 12 }}>{comm}</td>
                      <td style={{ padding: '10px 16px', fontFamily: "'SF Mono','Consolas',monospace", fontSize: 12, color: '#64748b' }}>
                        <span title={`UID: ${uid}`}>{pid}</span>
                      </td>
                      <td style={{ padding: '10px 16px' }}>
                        <span style={{
                          padding: '2px 8px', borderRadius: 10, fontSize: 12, fontWeight: 600,
                          background: ev.risk_contribution >= 0.6 ? '#fee2e2' : ev.risk_contribution >= 0.3 ? '#fef3c7' : '#d1fae5',
                          color: ev.risk_contribution >= 0.6 ? '#dc2626' : ev.risk_contribution >= 0.3 ? '#b45309' : '#047857',
                        }}>
                          {(ev.risk_contribution ?? 0).toFixed(2)}
                        </span>
                      </td>
                      <td style={{ padding: '10px 16px' }}>
                        {opaAllow !== undefined ? (
                          <span style={{
                            padding: '2px 8px', borderRadius: 4, fontSize: 11, fontWeight: 600,
                            background: opaAllow === 'true' ? '#d1fae5' : '#fee2e2',
                            color: opaAllow === 'true' ? '#047857' : '#dc2626',
                          }}>
                            {opaAllow === 'true' ? <Shield size={12} style={{ verticalAlign: 'middle', marginRight: 2 }} /> : <ShieldAlert size={12} style={{ verticalAlign: 'middle', marginRight: 2 }} />}
                            {opaAllow === 'true' ? '允许' : '拒绝'}
                          </span>
                        ) : (
                          <span style={{ fontSize: 11, color: '#94a3b8' }}>-</span>
                        )}
                        {riskLevel && (
                          <span style={{ ...(severityBadge[riskLevel] || severityBadge.low), padding: '1px 6px', borderRadius: 3, fontSize: 10, fontWeight: 600, marginLeft: 4 }}>
                            {riskLevel}
                          </span>
                        )}
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
