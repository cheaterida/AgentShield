package workflow

import (
	"sync"
	"time"

	"agentshield.dev/agentshield/error-handler/internal"
)

// Aggregator 将低级别告警聚合，超阈值时升级为 critical。
type Aggregator struct {
	mu           sync.Mutex
	buckets      map[string][]alertRecord // agentID -> recent low alerts
	threshold    int                      // 触发升级的告警数
	windowSecs   int                      // 时间窗口（秒）
}

type alertRecord struct {
	alert     internal.AlertInfo
	timestamp time.Time
}

func NewAggregator(threshold, windowSecs int) *Aggregator {
	return &Aggregator{
		buckets:    make(map[string][]alertRecord),
		threshold:  threshold,
		windowSecs: windowSecs,
	}
}

// Add 添加一条低级别告警。如果超过阈值，返回升级后的 critical 告警。
func (a *Aggregator) Add(alert internal.AlertInfo) *internal.AlertInfo {
	if alert.Severity != "low" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	now := time.Now()
	key := alert.AgentID
	if key == "" {
		key = alert.FamilyGroupID
	}

	// 清理过期记录
	cutoff := now.Add(-time.Duration(a.windowSecs) * time.Second)
	valid := make([]alertRecord, 0, len(a.buckets[key]))
	for _, r := range a.buckets[key] {
		if r.timestamp.After(cutoff) {
			valid = append(valid, r)
		}
	}
	valid = append(valid, alertRecord{alert: alert, timestamp: now})
	a.buckets[key] = valid

	if len(valid) >= a.threshold {
		delete(a.buckets, key)
		escalated := alert
		escalated.Severity = "critical"
		escalated.Title = "ESCALATED: " + alert.Title + " (threshold exceeded)"
		return &escalated
	}
	return nil
}
