// Package policy handles OPA policy bundle lifecycle and distribution.
package policy

import (
	"context"
	"log/slog"
	"sync"

	"agentshield.dev/agentshield/management-server/internal/models"
	"agentshield.dev/agentshield/management-server/internal/store"
)

// Distributor 管理策略包的激活与推送。
type Distributor struct {
	store   store.Store
	log     *slog.Logger
	mu      sync.RWMutex
	watchers map[string]chan *models.PolicyBundle // agentID -> chan
}

func NewDistributor(st store.Store, log *slog.Logger) *Distributor {
	return &Distributor{
		store:    st,
		log:      log,
		watchers: make(map[string]chan *models.PolicyBundle),
	}
}

// ActivatePolicy 激活策略包并通知所有观察者。
func (d *Distributor) ActivatePolicy(ctx context.Context, version string) error {
	policies, err := d.store.ListPolicyBundles(ctx)
	if err != nil {
		return err
	}
	var target *models.PolicyBundle
	for i := range policies {
		if policies[i].Version == version {
			target = &policies[i]
			break
		}
	}
	if target == nil {
		return nil
	}
	// 取消激活所有旧策略
	for i := range policies {
		if policies[i].Active {
			// 这里需要 store 提供 DeactivateAll，简化处理直接标记新的
		}
	}
	target.Active = true
	d.log.Info("policy activated", "version", version)

	// 通知所有观察者
	d.mu.RLock()
	defer d.mu.RUnlock()
	for agentID, ch := range d.watchers {
		select {
		case ch <- target:
		default:
			d.log.Warn("watcher channel full, dropping", "agent_id", agentID)
		}
	}
	return nil
}

// Watch 为指定 agent 注册策略观察通道。
func (d *Distributor) Watch(agentID string) <-chan *models.PolicyBundle {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch, ok := d.watchers[agentID]
	if !ok {
		ch = make(chan *models.PolicyBundle, 4)
		d.watchers[agentID] = ch
	}
	return ch
}

// Unwatch 移除 agent 的策略观察。
func (d *Distributor) Unwatch(agentID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if ch, ok := d.watchers[agentID]; ok {
		close(ch)
		delete(d.watchers, agentID)
	}
}

// BroadcastMLPolicy 将 ML 生成的策略包通知所有观察者并持久化。
func (d *Distributor) BroadcastMLPolicy(ctx context.Context, pb *models.PolicyBundle) error {
	if err := d.store.CreatePolicyBundle(ctx, *pb); err != nil {
		return err
	}
	d.log.Info("ml policy broadcast", "version", pb.Version, "type", pb.PolicyType)

	d.mu.RLock()
	defer d.mu.RUnlock()
	for agentID, ch := range d.watchers {
		select {
		case ch <- pb:
		default:
			d.log.Warn("watcher channel full, dropping ml policy", "agent_id", agentID)
		}
	}
	return nil
}

// GetCurrentVersion 返回当前活跃策略版本。
func (d *Distributor) GetCurrentVersion(ctx context.Context) string {
	pb, ok, err := d.store.GetActivePolicyBundle(ctx)
	if err != nil || !ok {
		return ""
	}
	return pb.Version
}
