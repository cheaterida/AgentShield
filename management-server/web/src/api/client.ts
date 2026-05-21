import type { TokenQuota, TokenUsageLog, TokenUsageSummary, ModelPrice } from './types';

const BASE = '/api/v1';

async function request<T>(url: string, options?: RequestInit): Promise<T> {
  const resp = await fetch(`${BASE}${url}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  });
  if (!resp.ok) {
    const err = await resp.json().catch(() => ({ error: resp.statusText }));
    throw new Error(err.error || `HTTP ${resp.status}`);
  }
  return resp.json();
}

export const api = {
  // Health
  healthz: () => request<{ status: string }>('/healthz'),

  // Agents
  registerAgent: (body: unknown) =>
    request('/agents/register', { method: 'POST', body: JSON.stringify(body) }),
  listAgents: (params?: string) =>
    request<{ agents: import('./types').Agent[] }>(`/agents${params ? `?${params}` : ''}`),
  getAgent: (id: string) =>
    request<import('./types').Agent>(`/agents/${encodeURIComponent(id)}`),
  updateAgentStatus: (id: string, status: string) =>
    request(`/agents/${encodeURIComponent(id)}/status`, {
      method: 'PUT',
      body: JSON.stringify({ status }),
    }),

  // Audit
  appendAuditEvents: (events: unknown[]) =>
    request('/audit/events', {
      method: 'POST',
      body: JSON.stringify({ events }),
    }),
  listAuditEvents: (params?: string) =>
    request<{ events: import('./types').AuditEvent[]; total: number }>(
      `/audit/events${params ? `?${params}` : ''}`
    ),

  // Family Groups
  listFamilyGroups: () =>
    request<{ groups: import('./types').FamilyGroup[] }>('/family-groups'),
  createFamilyGroup: (body: unknown) =>
    request('/family-groups', { method: 'POST', body: JSON.stringify(body) }),
  getFamilyGroup: (id: string) =>
    request<import('./types').FamilyGroup>(`/family-groups/${encodeURIComponent(id)}`),
  updateFamilyGroup: (id: string, body: unknown) =>
    request(`/family-groups/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteFamilyGroup: (id: string) =>
    request(`/family-groups/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // Policies
  listPolicyBundles: () =>
    request<{ bundles: import('./types').PolicyBundle[] }>('/policies/bundles'),
  createPolicyBundle: (body: unknown) =>
    request('/policies/bundles', { method: 'POST', body: JSON.stringify(body) }),
  activatePolicyBundle: (version: string) =>
    request(`/policies/bundles/${encodeURIComponent(version)}/activate`, { method: 'PUT' }),

  // Alerts
  listAlerts: (params?: string) =>
    request<{ alerts: import('./types').RiskAlert[]; total: number }>(
      `/alerts${params ? `?${params}` : ''}`
    ),
  updateAlert: (alertId: string, status: string) =>
    request(`/alerts/${encodeURIComponent(alertId)}`, {
      method: 'PUT',
      body: JSON.stringify({ status }),
    }),

  // Traces — real ClickHouse data via serve-web.py :8081
  // Vite proxies /api/v1/traces* to :8081, /api/* to :8080
  listTraces: (params?: string) =>
    request<{ traces: import('./types').TraceGroup[]; total: number }>(
      `/traces${params ? `?${params}` : ''}`
    ),
  listTracesByAgent: (agentId: string, params?: string) =>
    request<{ traces: import('./types').TraceGroup[]; total: number; agent_id: string; family_group_id: string }>(
      `/traces/by-agent?agent_id=${encodeURIComponent(agentId)}${params ? `&${params}` : ''}`
    ),
  listFamilyGroupsWithAgents: () =>
    request<{ groups: import('./types').FamilyGroupWithAgents[] }>('/family-groups-with-agents'),

  // Dashboard
  getDashboardStats: (familyGroupId?: string) =>
    request<import('./types').DashboardStats>(
      `/dashboard/stats${familyGroupId ? `?family_group_id=${encodeURIComponent(familyGroupId)}` : ''}`
    ),

  // Token Quota — Usage
  getAgentUsage: (id: string) =>
    request<Record<string, number>>(`/quota/agents/${encodeURIComponent(id)}/usage`),
  getFamilyGroupUsage: (id: string) =>
    request<{ summaries: TokenUsageSummary[] }>(`/quota/family-groups/${encodeURIComponent(id)}/usage`),
  getUsageSummary: (params?: string) =>
    request<{ summaries: TokenUsageSummary[] }>(`/quota/usage/summary${params ? `?${params}` : ''}`),

  // Token Quota — Model Prices
  listPrices: () =>
    request<{ prices: ModelPrice[] }>('/quota/prices'),
  upsertPrice: (body: Partial<ModelPrice>) =>
    request<ModelPrice>('/quota/prices', { method: 'POST', body: JSON.stringify(body) }),

  // Token Quota — Rules
  listQuotas: (params?: string) =>
    request<{ quotas: TokenQuota[] }>(`/quota/quotas${params ? `?${params}` : ''}`),
  createQuota: (body: Partial<TokenQuota>) =>
    request<TokenQuota>('/quota/quotas', { method: 'POST', body: JSON.stringify(body) }),
  updateQuota: (id: string, body: Partial<TokenQuota>) =>
    request<TokenQuota>(`/quota/quotas/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(body) }),
  deleteQuota: (id: string) =>
    request<{ deleted: boolean }>(`/quota/quotas/${encodeURIComponent(id)}`, { method: 'DELETE' }),

  // Token Quota — Logs
  listUsageLogs: (params?: string) =>
    request<{ logs: TokenUsageLog[]; total: number }>(`/quota/logs${params ? `?${params}` : ''}`),
};
