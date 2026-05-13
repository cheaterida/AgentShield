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
  attributes: Record<string, string>;
  risk_contribution: number;
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

export interface PolicyBundle {
  version: string;
  digest: string;
  active: boolean;
  created_at: string;
}
