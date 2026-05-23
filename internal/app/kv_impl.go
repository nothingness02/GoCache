package app

import (
	"context"

	"Flux-KV/internal/storage"
	"Flux-KV/pkg/metrics"
)

// kvUseCase 实现 KVUseCase 接口
type kvUseCase struct {
	db   storage.Engine
	meta NodeMeta
}

// NewKVUseCase 创建 KVUseCase 实例
func NewKVUseCase(db storage.Engine, meta NodeMeta) KVUseCase {
	return &kvUseCase{
		db:   db,
		meta: meta,
	}
}

// Set 写入键值
func (u *kvUseCase) Set(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := u.db.Set(key, value, 0); err != nil {
		metrics.KVRequestsTotal.WithLabelValues("set", "error").Inc()
		return err
	}
	metrics.KVSetTotal.Inc()
	metrics.KVRequestsTotal.WithLabelValues("set", "success").Inc()
	return nil
}

// Get 读取键值
func (u *kvUseCase) Get(ctx context.Context, key string) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	val, found := u.db.Get(key)
	metrics.KVGetTotal.Inc()
	metrics.KVRequestsTotal.WithLabelValues("get", "success").Inc()
	return val, found, nil
}

// Del 删除键值
func (u *kvUseCase) Del(ctx context.Context, key string) error {
	if err := u.db.Delete(key); err != nil {
		metrics.KVRequestsTotal.WithLabelValues("del", "error").Inc()
		return err
	}
	metrics.KVRequestsTotal.WithLabelValues("del", "success").Inc()
	return nil
}

// InternalSet 节点间数据迁移专用写入（绕过 Raft，直接写入 engine）
func (u *kvUseCase) InternalSet(ctx context.Context, key, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// 直接写入底层引擎，不经过 Raft/AOF/事件总线
	if err := u.db.Set(key, value, 0); err != nil {
		return err
	}
	return nil
}

// Status 返回节点状态
func (u *kvUseCase) Status(ctx context.Context) (*StatusInfo, error) {
	stats := u.db.Stats()
	metrics.EngineMemoryBytes.WithLabelValues(stats.EngineType, u.meta.NodeID).Set(float64(stats.MemoryBytes))
	return &StatusInfo{
		NodeID:     u.meta.NodeID,
		Mode:       u.meta.Mode,
		EngineType: stats.EngineType,
		Stats:      stats,
		Healthy:    true,
	}, nil
}

// RaftInfo 返回 Raft 状态（仅 CP 模式有效）
func (u *kvUseCase) RaftInfo(ctx context.Context) (*RaftInfo, error) {
	cpStore, ok := u.db.(*storage.CPStorage)
	if !ok {
		return &RaftInfo{}, nil
	}
	status := cpStore.RaftStatus()
	return &RaftInfo{
		Role:         status.Role,
		Term:         status.Term,
		CommitIndex:  status.CommitIndex,
		LastApplied:  status.LastApplied,
		HealthyNodes: int32(status.HealthyNodes),
		Peers:        status.Peers,
	}, nil
}
