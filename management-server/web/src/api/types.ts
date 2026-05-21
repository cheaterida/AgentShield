export interface Agent {
  id: string;
  family_group_id: string;
  display_name: string;
  labels: Record<string, string>;
  status: 'online' | 'offline' | 'suspicious' | 'degraded' | 'unknown';
  risk_score: number;
  last_heartbeat_at: string | null;
  registered_at: string;
  updated_at: string;
}

export interface AuditEvent {
  event_id: string;
  occurred_at: string;
  family_group_id: string;
  agent_id: string;
  resource_ref: string;
  action: string;
  attributes: AuditEventAttributes;
  risk_contribution: number;
}

export interface AuditEventAttributes {
  // eBPF probe
  comm?: string;
  pid?: string;
  uid?: string;
  network_dst?: string;
  socket_create?: string;
  // OPA injected (Go backend)
  opa_allow?: string;
  opa_risk_level?: string;
  opa_deny_sensitive_path?: string;
  opa_deny_network?: string;
  opa_risky_write?: string;
  opa_matched_path?: string;
  // Span / trace linkage
  span_id?: string;
  trace_id?: string;
  duration?: string;
  // Open-ended passthrough
  [key: string]: string | undefined;
}

export interface RiskAlert {
  alert_id: string;
  family_group_id: string;
  agent_id: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  title: string;
  description: string;
  status: 'open' | 'acknowledged' | 'resolved' | 'dismissed';
  metadata: Record<string, string> | null;
  occurred_at: string;
  resolved_at: string | null;
  created_at: string;
}

export interface DashboardStats {
  agent_count: number;
  online_agent_count: number;
  suspicious_agent_count: number;
  event_rate_last_hour: number;
  open_alert_count: number;
  critical_alert_count: number;
  recent_alerts: RiskAlert[];
}

export interface FamilyGroup {
  id: string;
  display_name: string;
  member_principal_ids: string[];
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
}


export interface SpanEvent {
  name: string;
  attributes: Record<string, string>;
}

export interface TraceSpan {
  trace_id: string;
  span_id: string;
  parent_id: string;
  name: string;
  kind: number;
  start_time: string;
  end_time: string;
  duration: number;
  status_code: number;
  attributes: Record<string, string>;
  events: SpanEvent[];
  agent_id: string;
  family_group_id: string;
}

export interface TraceGroup {
  trace_id: string;
  span_count: number;
  earliest: string;
  latest: string;
  spans: TraceSpan[];
}

export interface AgentInfo {
  id: string;
  name?: string;
  display_name?: string;
  hostname?: string;
  status: string;
}

export interface FamilyGroupWithAgents {
  id: string;
  name: string;
  display_name?: string;
  agent_count: number;
  agents: AgentInfo[];
}


export interface PolicyBundle {
  version: string;
  digest: string;
  active: boolean;
  created_at: string;
}

// ── Token Quota ──

export interface TokenQuota {
  quota_id: string;
  target_type: 'agent' | 'family_group';
  target_id: string;
  quota_name: string;
  daily_limit: number;
  weekly_limit: number;
  monthly_limit: number;
  total_limit: number;
  per_request_limit: number;
  max_concurrency: number;
  warn_threshold: number;
  block_threshold: number;
  priority: number;
  active: boolean;
  created_at: string;
  updated_at: string;
  daily_used?: number;
  monthly_used?: number;
}

export interface TokenUsageLog {
  log_id: string;
  agent_id: string;
  family_group_id: string;
  span_id: string;
  trace_id: string;
  model_name: string;
  provider: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  cost_millicents: number;
  quota_status: 'ok' | 'warned' | 'blocked';
  occurred_at: string;
  created_at: string;
}

export interface TokenUsageSummary {
  target_type: string;
  target_id: string;
  period: string;
  date_key: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  request_count: number;
  cost_millicents: number;
  updated_at: string;
}

export interface ModelPrice {
  model_id: string;
  provider: string;
  display_name: string;
  input_price_millicents: number;
  output_price_millicents: number;
  cache_read_price_millicents: number;
  active: boolean;
  updated_at: string;
}
