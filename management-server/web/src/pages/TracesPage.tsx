import { useState, useEffect, useCallback } from 'react';
import { api } from '../api/client';
import type { TraceGroup, FamilyGroupWithAgents, TraceSpan, AuditEvent } from '../api/types';

interface MsgItem {
  role: string;
  content?: string;
  tool_calls?: { function?: { name: string; arguments: string } }[];
}

function esc(s: string): string {
  const el = document.createElement('span');
  el.textContent = s;
  return el.innerHTML;
}

function parseMessages(raw: unknown): MsgItem[] | null {
  if (typeof raw === 'string') {
    try { return JSON.parse(raw); } catch { return null; }
  }
  if (Array.isArray(raw)) return raw as MsgItem[];
  return null;
}

function renderMessages(events: TraceSpan['events']) {
  if (!Array.isArray(events)) return null;

  const promptEv = events.find((e) => e.name === 'gen_ai.content.prompt');
  const completionEv = events.find((e) => e.name === 'gen_ai.content.completion');

  const renderBlock = (title: string, raw: unknown, color: string) => {
    const msgs = parseMessages(raw);
    if (!msgs || msgs.length === 0) return null;
    return (
      <div style={{ marginBottom: 12 }}>
        <div style={{ fontSize: 11, fontWeight: 700, color: '#64748b', marginBottom: 5 }}>
          {title}
        </div>
        {msgs.map((msg, i) => {
          const roleClass = `role-${msg.role || 'unknown'}` as keyof typeof roleColors;
          const content = Array.isArray(msg.content)
            ? msg.content.map((c) => (typeof c === 'object' ? JSON.stringify(c) : String(c))).join('\n')
            : msg.content || '';
          return (
            <div key={i} style={{ marginBottom: 8, padding: 8, background: '#f8fafc', borderRadius: 6, borderLeft: `3px solid ${color}` }}>
              <span style={roleBadgeStyle(roleClass)}>{msg.role?.toUpperCase() || 'UNKNOWN'}</span>
              {content && (
                <div style={{ fontSize: 12, lineHeight: 1.5, whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: '#334155', marginTop: 4 }}>
                  {content}
                </div>
              )}
              {msg.tool_calls?.map((tc, j) => (
                <div key={j} style={{ marginLeft: 12, padding: '4px 8px', background: '#fef3c7', borderRadius: 4, fontSize: 11, marginTop: 3 }}>
                  <strong>🔧 {tc.function?.name || 'tool'}</strong>
                  <br /><small>{tc.function?.arguments ? JSON.stringify(JSON.parse(tc.function.arguments), null, 2) : ''}</small>
                </div>
              ))}
            </div>
          );
        })}
      </div>
    );
  };

  return (
    <>
      {promptEv?.attributes?.['gen_ai.prompt'] && renderBlock('📥 Input (Prompt)', promptEv.attributes['gen_ai.prompt'], '#6366f1')}
      {completionEv?.attributes?.['gen_ai.completion'] && renderBlock('📤 Output (Completion)', completionEv.attributes['gen_ai.completion'], '#10b981')}
    </>
  );
}

const roleColors = {
  'role-system': '#be185d',
  'role-user': '#1d4ed8',
  'role-assistant': '#047857',
  'role-tool': '#b45309',
};

function roleBadgeStyle(role: keyof typeof roleColors): React.CSSProperties {
  return {
    display: 'inline-block',
    padding: '1px 6px',
    borderRadius: 3,
    fontSize: 10,
    fontWeight: 600,
    background:
      role === 'role-system' ? '#fce7f3' :
      role === 'role-user' ? '#dbeafe' :
      role === 'role-assistant' ? '#d1fae5' :
      role === 'role-tool' ? '#fef3c7' : '#f1f5f9',
    color: roleColors[role] || '#64748b',
  };
}

function groupEventsToTraces(events: AuditEvent[]): TraceGroup[] {
  const groups = new Map<string, TraceGroup>();
  for (const ev of events) {
    const traceId = ev.attributes?.trace_id || ev.event_id;
    let g = groups.get(traceId);
    if (!g) {
      g = {
        trace_id: traceId,
        span_count: 0,
        earliest: ev.occurred_at,
        latest: ev.occurred_at,
        spans: [],
      };
      groups.set(traceId, g);
    }
    const durMs = parseInt(ev.attributes?.duration || '0', 10) || 0;
    const attrs: Record<string, string> = {};
    if (ev.attributes) {
      for (const [k, v] of Object.entries(ev.attributes)) {
        if (v !== undefined) attrs[k] = v;
      }
    }
    const span: TraceSpan = {
      trace_id: traceId,
      span_id: ev.event_id,
      parent_id: '',
      name: `${ev.action} ${ev.resource_ref}`,
      kind: 0,
      start_time: ev.occurred_at,
      end_time: ev.occurred_at,
      duration: durMs,
      status_code: 0,
      attributes: attrs,
      events: [],
      agent_id: ev.agent_id,
      family_group_id: ev.family_group_id,
    };
    g.spans.push(span);
    g.span_count++;
    if (ev.occurred_at < g.earliest) g.earliest = ev.occurred_at;
    if (ev.occurred_at > g.latest) g.latest = ev.occurred_at;
  }
  return Array.from(groups.values());
}

const styles: Record<string, React.CSSProperties> = {
  container: { display: 'flex', height: 'calc(100vh - 48px)', background: '#f1f5f9', margin: -24 },
  sidebar: { width: 260, minWidth: 260, background: '#0f172a', color: '#e2e8f0', display: 'flex', flexDirection: 'column', overflow: 'hidden' },
  sidebarHeader: { padding: '14px 14px 10px', borderBottom: '1px solid #1e293b', fontSize: 15, fontWeight: 700 },
  sidebarNav: { flex: 1, overflowY: 'auto', padding: '6px 0' } as React.CSSProperties,
  navAll: { display: 'flex', alignItems: 'center', gap: 8, padding: '8px 14px', cursor: 'pointer', fontSize: 13, fontWeight: 600, borderLeft: '3px solid transparent', transition: 'all 0.15s' },
  groupHeader: { display: 'flex', alignItems: 'center', gap: 8, padding: '8px 14px', cursor: 'pointer', fontSize: 13, fontWeight: 600, color: '#94a3b8', transition: 'all 0.15s' },
  agentItem: { display: 'flex', alignItems: 'center', gap: 8, padding: '6px 14px 6px 36px', cursor: 'pointer', fontSize: 13, color: '#94a3b8', borderLeft: '3px solid transparent', transition: 'all 0.15s' },
  main: { flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' },
  mainHeader: { background: '#fff', padding: '12px 20px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderBottom: '1px solid #e2e8f0' },
  content: { flex: 1, overflowY: 'auto', padding: '16px 20px' } as React.CSSProperties,
  traceCard: { background: '#fff', borderRadius: 10, marginBottom: 8, boxShadow: '0 1px 3px rgba(0,0,0,0.06)', overflow: 'hidden' },
  traceHeader: { padding: '10px 16px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: 'pointer', borderBottom: '1px solid #f1f5f9' },
  spanItem: { padding: '8px 16px', borderBottom: '1px solid #f8fafc', cursor: 'pointer' },
  centerMsg: { textAlign: 'center', padding: '48px 20px', color: '#94a3b8' } as React.CSSProperties,
};

export function TracesPage() {
  const [tree, setTree] = useState<FamilyGroupWithAgents[]>([]);
  const [traces, setTraces] = useState<TraceGroup[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [selectedGroup, setSelectedGroup] = useState<string | null>(null);
  const [selectedName, setSelectedName] = useState('全部 Traces');
  const [expandedCards, setExpandedCards] = useState<Set<string>>(new Set());
  const [expandedSpans, setExpandedSpans] = useState<Set<string>>(new Set());
  const [openGroups, setOpenGroups] = useState<Set<string>>(new Set());

  const loadTree = useCallback(async () => {
    try {
      const [groupsData, agentsData] = await Promise.all([
        api.listFamilyGroupsWithAgents(),
        api.listAgents(),
      ]);
      const groups = (groupsData.groups || []).map((g) => {
        const groupAgents = (agentsData.agents || [])
          .filter((a) => a.family_group_id === g.id)
          .map((a) => ({
            id: a.id,
            name: a.display_name || a.id,
            hostname: a.labels?.hostname,
            status: a.status,
          }));
        return {
          id: g.id,
          name: g.display_name || g.id,
          agent_count: groupAgents.length,
          agents: groupAgents,
        };
      });
      setTree(groups);
      if (groups.length > 0) {
        setOpenGroups((prev) => { const next = new Set(prev); next.add(groups[0].id); return next; });
      }
    } catch { /* sidebar failure is non-fatal */ }
  }, []);

  const loadTraces = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.listTraces('limit=200');
      setTraces(groupEventsToTraces(data.events || []));
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadTracesByAgent = useCallback(async (agentId: string) => {
    setLoading(true);
    setError(null);
    try {
      const data = await api.listTraces(`limit=200&agent_id=${encodeURIComponent(agentId)}`);
      setTraces(groupEventsToTraces(data.events || []));
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadTree(); loadTraces(); }, [loadTree, loadTraces]);
  useEffect(() => {
    const interval = setInterval(() => {
      selectedAgent ? loadTracesByAgent(selectedAgent) : loadTraces();
    }, 10000);
    return () => clearInterval(interval);
  }, [selectedAgent, loadTraces, loadTracesByAgent]);

  const selectAll = () => {
    setSelectedAgent(null);
    setSelectedGroup(null);
    setSelectedName('全部 Traces');
    loadTraces();
  };

  const selectAgent = (agentId: string, groupId: string, name: string) => {
    setSelectedAgent(agentId);
    setSelectedGroup(groupId);
    setSelectedName(name);
    setOpenGroups((prev) => { const next = new Set(prev); next.add(groupId); return next; });
    loadTracesByAgent(agentId);
  };

  const toggleCard = (traceId: string) => {
    setExpandedCards((prev) => {
      const next = new Set(prev);
      next.has(traceId) ? next.delete(traceId) : next.add(traceId);
      return next;
    });
  };

  const toggleSpan = (spanId: string) => {
    setExpandedSpans((prev) => {
      const next = new Set(prev);
      next.has(spanId) ? next.delete(spanId) : next.add(spanId);
      return next;
    });
  };

  const toggleGroup = (groupId: string) => {
    setOpenGroups((prev) => {
      const next = new Set(prev);
      next.has(groupId) ? next.delete(groupId) : next.add(groupId);
      return next;
    });
  };

  const renderContent = () => {
    if (loading && traces.length === 0) {
      return (
        <div style={styles.centerMsg}>
          <div style={{ width: 24, height: 24, border: '3px solid #e2e8f0', borderTopColor: '#6366f1', borderRadius: '50%', animation: 'spin 0.8s linear infinite', margin: '0 auto 12px' }} />
          <div style={{ fontSize: 14 }}>加载中...</div>
        </div>
      );
    }
    if (error) {
      return (
        <div style={{ ...styles.centerMsg, background: '#fef2f2', color: '#dc2626', borderRadius: 8, margin: '16px 0' }}>
          加载失败: {error}
        </div>
      );
    }
    if (traces.length === 0) {
      return (
        <div style={styles.centerMsg}>
          暂无 trace 数据
          <div style={{ fontSize: 12, marginTop: 6 }}>请确保 agent 正在运行并产生 LLM 调用</div>
        </div>
      );
    }

    return traces.map((trace) => {
      const spans = trace.spans || [];
      const firstSpan = spans[0];
      const attrs = firstSpan?.attributes || {};
      const serviceType = attrs['langtrace.service.type'] || attrs['agentshield.service.type'] || '';
      const isOpen = expandedCards.has(trace.trace_id);

      return (
        <div key={trace.trace_id} style={styles.traceCard}>
          <div
            style={{ ...styles.traceHeader, background: isOpen ? '#f8fafc' : undefined }}
            onClick={() => toggleCard(trace.trace_id)}
          >
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flex: 1, minWidth: 0 }}>
              <span style={{
                display: 'inline-block', padding: '1px 6px', borderRadius: 3, fontSize: 10, fontWeight: 600,
                background: serviceType === 'llm' ? '#ede9fe' : serviceType === 'framework' ? '#dbeafe' : '#f1f5f9',
                color: serviceType === 'llm' ? '#7c3aed' : serviceType === 'framework' ? '#2563eb' : '#64748b',
              }}>
                {serviceType || 'span'}
              </span>
              <span style={{ fontFamily: "'SF Mono','Consolas',monospace", fontSize: 12, color: '#6366f1' }}>
                {trace.trace_id}
              </span>
              <span style={{ background: '#e2e8f0', padding: '2px 8px', borderRadius: 10, fontSize: 11, fontWeight: 600 }}>
                {spans.length} span{spans.length > 1 ? 's' : ''}
              </span>
            </div>
            <div style={{ fontSize: 12, color: '#64748b', flexShrink: 0 }}>{trace.earliest || ''}</div>
          </div>
          {isOpen && (
            <div>
              {spans.map((s) => {
                const a = s.attributes || {};
                const model = a['gen_ai.request.model'] || a['gen_ai.response.model'] || '';
                const duration = s.duration ? `${(s.duration / 1000).toFixed(2)}s` : '';
                const hasContent = Array.isArray(s.events) && s.events.length > 0;
                const isSpanExpanded = expandedSpans.has(s.span_id);
                const tags = ['gen_ai.operation.name', 'gen_ai.system', 'langtrace.service.name']
                  .filter((k) => a[k])
                  .map((k) => ({ key: k.split('.').pop()!, value: a[k] }));

                return (
                  <div
                    key={s.span_id}
                    style={{ ...styles.spanItem, background: isSpanExpanded ? '#fffbeb' : undefined }}
                    onClick={() => hasContent && toggleSpan(s.span_id)}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 13, flexWrap: 'wrap' }}>
                      <span style={{ fontWeight: 600, minWidth: 180 }}>{s.name}</span>
                      <span style={{ fontSize: 12, color: '#f59e0b', minWidth: 70 }}>{duration}</span>
                      <span style={{ fontSize: 11, color: '#94a3b8', minWidth: 160 }}>{s.start_time || ''}</span>
                      <span style={{ fontSize: 11, color: '#6366f1' }}>{model}</span>
                      <span style={{ display: 'flex', flexWrap: 'wrap', gap: 4, flex: 1 }}>
                        {tags.map((t) => (
                          <span key={t.key} style={{ background: '#f1f5f9', padding: '1px 6px', borderRadius: 3, fontSize: 10, color: '#475569' }}>
                            {t.key}: {t.value}
                          </span>
                        ))}
                      </span>
                      {hasContent && <span style={{ fontSize: 10, color: '#94a3b8' }}>▶ 点击查看内容</span>}
                    </div>
                    {isSpanExpanded && hasContent && (
                      <div style={{ marginTop: 10, paddingTop: 10, borderTop: '1px solid #e2e8f0' }}>
                        {renderMessages(s.events)}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      );
    });
  };

  return (
    <div style={styles.container}>
      {/* Sidebar */}
      <div style={styles.sidebar}>
        <div style={styles.sidebarHeader}>🔍 链路追踪</div>
        <div style={styles.sidebarNav}>
          <div
            style={{
              ...styles.navAll,
              background: !selectedAgent ? '#1e293b' : 'transparent',
              borderLeftColor: !selectedAgent ? '#6366f1' : 'transparent',
              color: !selectedAgent ? '#a5b4fc' : '#e2e8f0',
            }}
            onClick={selectAll}
          >
            📊 全部 Traces
          </div>
          {tree.map((g) => {
            const isOpen = openGroups.has(g.id);
            return (
              <div key={g.id}>
                <div
                  style={styles.groupHeader}
                  onClick={() => toggleGroup(g.id)}
                  onMouseEnter={(e) => { e.currentTarget.style.background = '#1e293b'; e.currentTarget.style.color = '#cbd5e1'; }}
                  onMouseLeave={(e) => { e.currentTarget.style.background = ''; e.currentTarget.style.color = ''; }}
                >
                  <span style={{ fontSize: 10, width: 12, textAlign: 'center', transform: isOpen ? 'rotate(90deg)' : undefined, transition: 'transform 0.2s' }}>▶</span>
                  <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={esc(g.name)}>🏠 {g.name}</span>
                  <span style={{ fontSize: 11, color: '#64748b' }}>{g.agent_count}</span>
                </div>
                {isOpen && (
                  <div>
                    {g.agents?.map((a) => (
                      <div
                        key={a.id}
                        style={{
                          ...styles.agentItem,
                          background: selectedAgent === a.id ? '#1e293b' : 'transparent',
                          borderLeftColor: selectedAgent === a.id ? '#6366f1' : 'transparent',
                          color: selectedAgent === a.id ? '#c7d2fe' : '#94a3b8',
                        }}
                        onClick={() => selectAgent(a.id, g.id, a.name || a.hostname || a.id)}
                        onMouseEnter={(e) => { if (selectedAgent !== a.id) { e.currentTarget.style.background = '#1e293b'; e.currentTarget.style.color = '#e2e8f0'; } }}
                        onMouseLeave={(e) => { if (selectedAgent !== a.id) { e.currentTarget.style.background = ''; e.currentTarget.style.color = ''; } }}
                      >
                        <span style={{
                          width: 7, height: 7, borderRadius: '50%', flexShrink: 0,
                          background: a.status === 'online' ? '#22c55e' : a.status === 'offline' ? '#ef4444' : '#64748b',
                        }} />
                        <span style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={esc(a.name || a.hostname || a.id)}>
                          {a.name || a.hostname || a.id}
                        </span>
                      </div>
                    ))}
                    {(!g.agents || g.agents.length === 0) && (
                      <div style={{ padding: '4px 14px 4px 36px', fontSize: 12, color: '#64748b' }}>暂无 Agent</div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </div>

      {/* Main */}
      <div style={styles.main}>
        <div style={styles.mainHeader}>
          <div>
            <span style={{ fontSize: 16, fontWeight: 700 }}>{selectedName}</span>
            {selectedAgent && <span style={{ fontSize: 12, color: '#64748b', marginLeft: 8 }}>Agent: {selectedAgent}</span>}
          </div>
          <div style={{ fontSize: 12, color: '#64748b' }}>自动刷新 · 10s</div>
        </div>
        <div style={styles.content}>
          {renderContent()}
        </div>
      </div>

      <style>{`@keyframes spin { to { transform: rotate(360deg); } }`}</style>
    </div>
  );
}
