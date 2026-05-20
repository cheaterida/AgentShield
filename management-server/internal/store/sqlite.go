package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"agentshield.dev/agentshield/management-server/internal/models"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLite(dsn string) (*SQLiteStore, error) {
	// modernc.org/sqlite requires file: URI format with mode=rwc for absolute paths
	if dsn != ":memory:" && !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn + "?mode=rwc"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite serializes writes
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("sqlite pragma: %w", err)
	}
	s := &SQLiteStore{db: db}
	if err := s.runMigrations(); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) runMigrations() error {
	for _, name := range []string{"migrations/001_init.sql", "migrations/002_add_policy_metadata.sql"} {
		data, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := s.db.Exec(string(data)); err != nil {
			// Column-already-exists errors are non-fatal for ALTER TABLE
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			return err
		}
	}
	return nil
}

// ── helpers ──

func jsonMap(b []byte) map[string]string {
	if len(b) == 0 {
		return nil
	}
	var m map[string]string
	_ = json.Unmarshal(b, &m)
	return m
}

func marshalMap(m map[string]string) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, _ := json.Marshal(m)
	return b
}

// ── FamilyGroup ──

func (s *SQLiteStore) CreateFamilyGroup(_ context.Context, fg models.FamilyGroup) error {
	if fg.ID == "" {
		return errors.New("family_group id required")
	}
	members, _ := json.Marshal(fg.MemberPrincipalIDs)
	_, err := s.db.Exec(
		`INSERT INTO family_groups (id, display_name, member_principal_ids, labels, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		fg.ID, fg.DisplayName, string(members), marshalMap(fg.Labels), time.Now().UTC().Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetFamilyGroup(_ context.Context, id string) (models.FamilyGroup, bool, error) {
	var fg models.FamilyGroup
	var labels []byte
	var members []byte
	var ca, ua string
	err := s.db.QueryRow(`SELECT id, display_name, COALESCE(member_principal_ids,'[]'), labels, created_at, updated_at FROM family_groups WHERE id=?`, id).
		Scan(&fg.ID, &fg.DisplayName, &members, &labels, &ca, &ua)
	if errors.Is(err, sql.ErrNoRows) {
		debugLog("GetFamilyGroup not found", "id", id, "store", "sqlite")
		return fg, false, nil
	}
	if err != nil {
		return fg, false, err
	}
	fg.Labels = jsonMap(labels)
	_ = json.Unmarshal(members, &fg.MemberPrincipalIDs)
	fg.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	fg.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
	return fg, true, nil
}

func (s *SQLiteStore) ListFamilyGroups(_ context.Context) ([]models.FamilyGroup, error) {
	rows, err := s.db.Query(`SELECT id, display_name, COALESCE(member_principal_ids,'[]'), labels, created_at, updated_at FROM family_groups ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.FamilyGroup
	for rows.Next() {
		var fg models.FamilyGroup
		var labels, members []byte
		var ca, ua string
		if err := rows.Scan(&fg.ID, &fg.DisplayName, &members, &labels, &ca, &ua); err != nil {
			return out, err
		}
		fg.Labels = jsonMap(labels)
		_ = json.Unmarshal(members, &fg.MemberPrincipalIDs)
		fg.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		fg.UpdatedAt, _ = time.Parse(time.RFC3339Nano, ua)
		out = append(out, fg)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) UpdateFamilyGroup(_ context.Context, fg models.FamilyGroup) error {
	members, _ := json.Marshal(fg.MemberPrincipalIDs)
	results, err := s.db.Exec(
		`UPDATE family_groups SET display_name=?, member_principal_ids=?, labels=?, updated_at=? WHERE id=?`,
		fg.DisplayName, string(members), marshalMap(fg.Labels), time.Now().UTC().Format(time.RFC3339Nano), fg.ID,
	)
	if err != nil {
		return err
	}
	n, _ := results.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) DeleteFamilyGroup(_ context.Context, id string) error {
	results, err := s.db.Exec(`DELETE FROM family_groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := results.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Agent ──

func (s *SQLiteStore) UpsertAgent(_ context.Context, a models.Agent) error {
	if a.ID == "" {
		return errors.New("agent id required")
	}
	if a.FamilyGroupID == "" {
		return errors.New("family_group_id required")
	}
	now := time.Now().UTC()
	if a.Status == "" {
		a.Status = "unknown"
	}
	existing, found, _ := s.GetAgent(context.Background(), a.ID)
	if found {
		if a.DisplayName == "" {
			a.DisplayName = existing.DisplayName
		}
		if len(a.Labels) == 0 {
			a.Labels = existing.Labels
		}
		if a.RegisteredAt.IsZero() {
			a.RegisteredAt = existing.RegisteredAt
		}
		_, err := s.db.Exec(
			`UPDATE agents SET family_group_id=?, display_name=?, labels=?, status=?, risk_score=?, last_heartbeat_at=?, updated_at=? WHERE id=?`,
			a.FamilyGroupID, a.DisplayName, marshalMap(a.Labels), a.Status, a.RiskScore,
			formatTime(a.LastHeartbeatAt), now.Format(time.RFC3339Nano), a.ID,
		)
		return err
	}
	if a.RegisteredAt.IsZero() {
		a.RegisteredAt = now
	}
	_, err := s.db.Exec(
		`INSERT INTO agents (id, family_group_id, display_name, labels, status, risk_score, last_heartbeat_at, registered_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		a.ID, a.FamilyGroupID, a.DisplayName, marshalMap(a.Labels), a.Status, a.RiskScore,
		formatTime(a.LastHeartbeatAt), a.RegisteredAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) GetAgent(_ context.Context, id string) (models.Agent, bool, error) {
	var a models.Agent
	var labels []byte
	var reg, upd string
	var lhb *string
	err := s.db.QueryRow(
		`SELECT id, family_group_id, display_name, labels, status, risk_score, last_heartbeat_at, registered_at, updated_at FROM agents WHERE id=?`,
		id,
	).Scan(&a.ID, &a.FamilyGroupID, &a.DisplayName, &labels, &a.Status, &a.RiskScore, &lhb, &reg, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	if err != nil {
		return a, false, err
	}
	a.Labels = jsonMap(labels)
	a.RegisteredAt, _ = time.Parse(time.RFC3339Nano, reg)
	a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, upd)
	a.LastHeartbeatAt = parseTimePtr(lhb)
	return a, true, nil
}

func (s *SQLiteStore) ListAgents(_ context.Context, familyGroupID string) ([]models.Agent, error) {
	var rows *sql.Rows
	var err error
	if familyGroupID != "" {
		rows, err = s.db.Query(`SELECT id, family_group_id, display_name, labels, status, risk_score, last_heartbeat_at, registered_at, updated_at FROM agents WHERE family_group_id=? ORDER BY updated_at DESC`, familyGroupID)
	} else {
		rows, err = s.db.Query(`SELECT id, family_group_id, display_name, labels, status, risk_score, last_heartbeat_at, registered_at, updated_at FROM agents ORDER BY updated_at DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (s *SQLiteStore) ListAgentsByStatus(_ context.Context, status string) ([]models.Agent, error) {
	rows, err := s.db.Query(`SELECT id, family_group_id, display_name, labels, status, risk_score, last_heartbeat_at, registered_at, updated_at FROM agents WHERE status=? ORDER BY updated_at DESC`, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAgents(rows)
}

func (s *SQLiteStore) UpdateAgentStatus(_ context.Context, id string, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE agents SET status=?, updated_at=? WHERE id=?`, status, now, id)
	return err
}

func (s *SQLiteStore) UpdateAgentHeartbeat(_ context.Context, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE agents SET last_heartbeat_at=?, status='online', updated_at=? WHERE id=?`, now, now, id)
	return err
}

func (s *SQLiteStore) UpdateAgentRiskScore(_ context.Context, id string, score float64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`UPDATE agents SET risk_score=?, updated_at=? WHERE id=?`, score, now, id)
	return err
}

// ── Audit events ──

func (s *SQLiteStore) AppendAuditEvents(_ context.Context, events []models.AuditEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO audit_events (event_id, occurred_at, family_group_id, agent_id, resource_ref, action, attributes, risk_contribution) VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for _, e := range events {
		if e.EventID == "" || e.AgentID == "" {
			continue
		}
		ts := e.OccurredAt
		if ts.IsZero() {
			ts = time.Now().UTC()
		}
		_, err := stmt.Exec(e.EventID, ts.Format(time.RFC3339Nano), e.FamilyGroupID, e.AgentID, e.ResourceRef, e.Action, marshalMap(e.Attributes), e.RiskContribution)
		if err != nil {
			continue // duplicate event_id
		}
		n++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *SQLiteStore) ListAuditEvents(_ context.Context, limit int) ([]models.AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT event_id, occurred_at, family_group_id, agent_id, resource_ref, action, attributes, risk_contribution FROM audit_events ORDER BY occurred_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAuditEvents(rows)
}

func (s *SQLiteStore) ListAuditEventsFiltered(_ context.Context, filter models.AuditEventFilter) ([]models.AuditEvent, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if filter.AgentID != "" {
		where = append(where, "agent_id=?")
		args = append(args, filter.AgentID)
	}
	if filter.FamilyGroupID != "" {
		where = append(where, "family_group_id=?")
		args = append(args, filter.FamilyGroupID)
	}
	if filter.Action != "" {
		where = append(where, "action=?")
		args = append(args, filter.Action)
	}
	if filter.FromTime != nil {
		where = append(where, "occurred_at>=?")
		args = append(args, filter.FromTime.Format(time.RFC3339Nano))
	}
	if filter.ToTime != nil {
		where = append(where, "occurred_at<?")
		args = append(args, filter.ToTime.Format(time.RFC3339Nano))
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_events WHERE %s", strings.Join(where, " AND "))
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf("SELECT event_id, occurred_at, family_group_id, agent_id, resource_ref, action, attributes, risk_contribution FROM audit_events WHERE %s ORDER BY occurred_at DESC LIMIT ? OFFSET ?", strings.Join(where, " AND "))
	rows, err := s.db.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	events, err := scanAuditEvents(rows)
	return events, total, err
}

// ── Policy bundles ──

func (s *SQLiteStore) CreatePolicyBundle(_ context.Context, pb models.PolicyBundle) error {
	metadataJSON := "{}"
	if len(pb.Metadata) > 0 {
		b, _ := json.Marshal(pb.Metadata)
		metadataJSON = string(b)
	}
	pt := pb.PolicyType
	if pt == "" {
		pt = "opa_rego"
	}
	_, err := s.db.Exec(`INSERT INTO policy_bundles (version, policy_type, payload, digest, active, metadata_json, created_at) VALUES (?,?,?,?,?,?,?)`,
		pb.Version, pt, pb.Payload, pb.Digest, pb.Active, metadataJSON, time.Now().UTC().Format(time.RFC3339Nano))
	if err == nil {
		debugLog("CreatePolicyBundle", "version", pb.Version, "active", pb.Active, "store", "sqlite")
	}
	return err
}

func (s *SQLiteStore) GetActivePolicyBundle(_ context.Context) (models.PolicyBundle, bool, error) {
	var pb models.PolicyBundle
	var ca, metaStr string
	err := s.db.QueryRow(`SELECT version, policy_type, payload, digest, active, COALESCE(metadata_json,'{}'), created_at FROM policy_bundles WHERE active=1 ORDER BY created_at DESC LIMIT 1`).
		Scan(&pb.Version, &pb.PolicyType, &pb.Payload, &pb.Digest, &pb.Active, &metaStr, &ca)
	if errors.Is(err, sql.ErrNoRows) {
		debugLog("GetActivePolicyBundle: no active bundle found", "store", "sqlite")
		return pb, false, nil
	}
	if err != nil {
		return pb, false, err
	}
	pb.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
	if metaStr != "" && metaStr != "{}" {
		_ = json.Unmarshal([]byte(metaStr), &pb.Metadata)
	}
	return pb, true, nil
}

func (s *SQLiteStore) SetPolicyBundleActive(_ context.Context, version string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE policy_bundles SET active = 0`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE policy_bundles SET active = 1 WHERE version = ?`, version); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ListPolicyBundles(_ context.Context) ([]models.PolicyBundle, error) {
	rows, err := s.db.Query(`SELECT version, policy_type, payload, digest, active, COALESCE(metadata_json,'{}'), created_at FROM policy_bundles ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.PolicyBundle
	for rows.Next() {
		var pb models.PolicyBundle
		var ca, metaStr string
		if err := rows.Scan(&pb.Version, &pb.PolicyType, &pb.Payload, &pb.Digest, &pb.Active, &metaStr, &ca); err != nil {
			return out, err
		}
		pb.CreatedAt, _ = time.Parse(time.RFC3339Nano, ca)
		if metaStr != "" && metaStr != "{}" {
			_ = json.Unmarshal([]byte(metaStr), &pb.Metadata)
		}
		out = append(out, pb)
	}
	return out, rows.Err()
}

// ── Risk alerts ──

func (s *SQLiteStore) CreateRiskAlert(_ context.Context, alert models.RiskAlert) error {
	meta, _ := json.Marshal(alert.Metadata)
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO risk_alerts (alert_id, family_group_id, agent_id, severity, title, description, status, metadata_json, occurred_at, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		alert.AlertID, alert.FamilyGroupID, alert.AgentID, alert.Severity, alert.Title, alert.Description, alert.Status, string(meta), alert.OccurredAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	return err
}

func (s *SQLiteStore) ListRiskAlerts(_ context.Context, filter models.AlertFilter) ([]models.RiskAlert, int, error) {
	where := []string{"1=1"}
	args := []any{}
	if filter.FamilyGroupID != "" {
		where = append(where, "family_group_id=?")
		args = append(args, filter.FamilyGroupID)
	}
	if filter.Severity != "" {
		where = append(where, "severity=?")
		args = append(args, filter.Severity)
	}
	if filter.Status != "" {
		where = append(where, "status=?")
		args = append(args, filter.Status)
	}

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM risk_alerts WHERE %s", strings.Join(where, " AND "))
	if err := s.db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf("SELECT alert_id, family_group_id, COALESCE(agent_id,''), severity, title, description, status, metadata_json, occurred_at, COALESCE(resolved_at,''), created_at FROM risk_alerts WHERE %s ORDER BY occurred_at DESC LIMIT ? OFFSET ?", strings.Join(where, " AND "))
	rows, err := s.db.Query(query, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.RiskAlert
	for rows.Next() {
		var a models.RiskAlert
		var meta []byte
		var occ, res, cre string
		if err := rows.Scan(&a.AlertID, &a.FamilyGroupID, &a.AgentID, &a.Severity, &a.Title, &a.Description, &a.Status, &meta, &occ, &res, &cre); err != nil {
			return out, total, err
		}
		_ = json.Unmarshal(meta, &a.Metadata)
		a.OccurredAt, _ = time.Parse(time.RFC3339Nano, occ)
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, cre)
		a.ResolvedAt = parseTimePtr(&res)
		out = append(out, a)
	}
	return out, total, rows.Err()
}

func (s *SQLiteStore) UpdateRiskAlertStatus(_ context.Context, alertID string, status string) error {
	var resolvedAt *string
	if status == "resolved" || status == "dismissed" {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		resolvedAt = &now
	}
	_, err := s.db.Exec(`UPDATE risk_alerts SET status=?, resolved_at=? WHERE alert_id=?`, status, resolvedAt, alertID)
	return err
}

// ── Dashboard ──

func (s *SQLiteStore) GetDashboardStats(_ context.Context, familyGroupID string) (models.DashboardStats, error) {
	var ds models.DashboardStats
	whereClause := "1=1"
	arg := any(nil)
	if familyGroupID != "" {
		whereClause = "family_group_id=?"
		arg = familyGroupID
	}

	s.db.QueryRow("SELECT COUNT(*) FROM agents WHERE "+whereClause, arg).Scan(&ds.AgentCount)
	s.db.QueryRow("SELECT COUNT(*) FROM agents WHERE status='online' AND "+whereClause, arg).Scan(&ds.OnlineAgentCount)
	s.db.QueryRow("SELECT COUNT(*) FROM agents WHERE status='suspicious' AND "+whereClause, arg).Scan(&ds.SuspiciousAgentCount)

	oneHourAgo := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	s.db.QueryRow("SELECT COUNT(*) FROM audit_events WHERE occurred_at >= ? AND "+whereClause, oneHourAgo, arg).Scan(&ds.EventRateLastHour)
	ds.EventRateLastHour = ds.EventRateLastHour / 60 // events per minute

	s.db.QueryRow("SELECT COUNT(*) FROM risk_alerts WHERE status='open' AND "+whereClause, arg).Scan(&ds.OpenAlertCount)
	s.db.QueryRow("SELECT COUNT(*) FROM risk_alerts WHERE severity='critical' AND status='open' AND "+whereClause, arg).Scan(&ds.CriticalAlertCount)

	rows, err := s.db.Query(
		"SELECT alert_id, family_group_id, COALESCE(agent_id,''), severity, title, description, status, metadata_json, occurred_at, COALESCE(resolved_at,''), created_at FROM risk_alerts WHERE status='open' AND "+whereClause+" ORDER BY occurred_at DESC LIMIT 5",
		arg,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var a models.RiskAlert
			var meta []byte
			var occ, res, cre string
			if err := rows.Scan(&a.AlertID, &a.FamilyGroupID, &a.AgentID, &a.Severity, &a.Title, &a.Description, &a.Status, &meta, &occ, &res, &cre); err != nil {
				continue
			}
			_ = json.Unmarshal(meta, &a.Metadata)
			a.OccurredAt, _ = time.Parse(time.RFC3339Nano, occ)
			a.CreatedAt, _ = time.Parse(time.RFC3339Nano, cre)
			a.ResolvedAt = parseTimePtr(&res)
			ds.RecentAlerts = append(ds.RecentAlerts, a)
		}
	}
	return ds, nil
}

// ── scan helpers ──

func scanAgents(rows *sql.Rows) ([]models.Agent, error) {
	var out []models.Agent
	for rows.Next() {
		var a models.Agent
		var labels []byte
		var reg, upd string
		var lhb *string
		if err := rows.Scan(&a.ID, &a.FamilyGroupID, &a.DisplayName, &labels, &a.Status, &a.RiskScore, &lhb, &reg, &upd); err != nil {
			return out, err
		}
		a.Labels = jsonMap(labels)
		a.RegisteredAt, _ = time.Parse(time.RFC3339Nano, reg)
		a.UpdatedAt, _ = time.Parse(time.RFC3339Nano, upd)
		a.LastHeartbeatAt = parseTimePtr(lhb)
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAuditEvents(rows *sql.Rows) ([]models.AuditEvent, error) {
	var out []models.AuditEvent
	for rows.Next() {
		var e models.AuditEvent
		var attr []byte
		var ts string
		if err := rows.Scan(&e.EventID, &ts, &e.FamilyGroupID, &e.AgentID, &e.ResourceRef, &e.Action, &attr, &e.RiskContribution); err != nil {
			return out, err
		}
		e.Attributes = jsonMap(attr)
		e.OccurredAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, e)
	}
	return out, rows.Err()
}

func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339Nano)
	return &s
}

func parseTimePtr(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, *s)
	if err != nil {
		return nil
	}
	return &t
}
