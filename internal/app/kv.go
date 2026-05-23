package app

import (
	"context"

	"Flux-KV/internal/storage"
)

// KVUseCase 定义 KV 核心业务逻辑接口
type KVUseCase interface {
	Set(ctx context.Context, key, value string) error
	Get(ctx context.Context, key string) (string, bool, error)
	Del(ctx context.Context, key string) error
	InternalSet(ctx context.Context, key, value string) error
	Status(ctx context.Context) (*StatusInfo, error)
	RaftInfo(ctx context.Context) (*RaftInfo, error)
}

// StatusInfo 节点状态信息
type StatusInfo struct {
	NodeID     string
	Mode       string
	EngineType string
	Stats      storage.EngineStats
	Healthy    bool
}

// RaftInfo Raft 集群信息
type RaftInfo struct {
	Role         string
	Term         uint64
	CommitIndex  uint64
	LastApplied  uint64
	HealthyNodes int32
	Peers        []string
}

// NodeMeta 存储节点元信息
type NodeMeta struct {
	NodeID string
	Mode   string // "cp" | "ap"
}
