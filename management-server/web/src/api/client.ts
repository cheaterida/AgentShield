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

  // Dashboard
  getDashboardStats: (familyGroupId?: string) =>
    request<import('./types').DashboardStats>(
      `/dashboard/stats${familyGroupId ? `?family_group_id=${encodeURIComponent(familyGroupId)}` : ''}`
    ),
};
