package storage

import (
	"Flux-KV/internal/raft"
	"errors"
	"time"
)

// CPStorage 包装 Engine + RaftNode，提供强一致性（CP）
type CPStorage struct {
	engine Engine
	node   *raft.RaftNode
}

// NewCPStorage 创建 CP 模式存储
func NewCPStorage(engine Engine, raftCfg *raft.Config) (*CPStorage, error) {
	cp := &CPStorage{engine: engine}
	node, err := raft.NewNode(raftCfg, cp)
	if err != nil {
		return nil, err
	}
	cp.node = node

	// 启动 Raft 节点
	node.Start()

	return cp, nil
}

// Node 返回底层 Raft 节点（用于注册 gRPC 服务）
func (cp *CPStorage) Node() *raft.RaftNode {
	return cp.node
}

// Transport 返回 Raft 传输层（用于外部访问）
func (cp *CPStorage) Transport() raft.Transport {
	return cp.node.Transport()
}

// ApplySet 实现 raft.ApplyStorage 接口，将已提交的日志应用到存储引擎
func (cp *CPStorage) ApplySet(key, value string, ttl int64) {
	var d time.Duration
	if ttl > 0 {
		d = time.Duration(ttl) * time.Millisecond
	}
	cp.engine.Set(key, value, d)
}

// ApplyDelete 实现 raft.ApplyStorage 接口
func (cp *CPStorage) ApplyDelete(key string) {
	cp.engine.Delete(key)
}

// Get 读取数据，Leader 先执行 ReadIndex 保证线性化
func (cp *CPStorage) Get(key string) (string, bool) {
	// 尝试线性化读，如果失败（如非 Leader 或超时），降级为本地读
	if err := cp.node.ReadIndex(5 * time.Second); err != nil {
		// 降级：直接读本地（非 Leader 时可能读到旧数据）
	}
	return cp.engine.Get(key)
}

// Set 通过 Raft 共识提交写操作
func (cp *CPStorage) Set(key string, value string, ttl time.Duration) error {
	cmd := raft.Command{
		Op:    "set",
		Key:   key,
		Value: value,
		TTL:   int64(ttl / time.Millisecond),
	}
	_, err := cp.node.Propose(cmd)
	if err != nil {
		return err
	}
	return nil
}

// Delete 通过 Raft 共识提交删除操作
func (cp *CPStorage) Delete(key string) error {
	cmd := raft.Command{
		Op:  "del",
		Key: key,
	}
	_, err := cp.node.Propose(cmd)
	if err != nil {
		return err
	}
	return nil
}

// Stats 返回引擎统计信息
func (cp *CPStorage) Stats() EngineStats {
	stats := cp.engine.Stats()
	stats.EngineType = "cp-" + stats.EngineType
	return stats
}

// Close 关闭 CP 存储
func (cp *CPStorage) Close() error {
	cp.node.Stop()
	return cp.engine.Close()
}

// Scan 遍历所有未过期的 key-value（委托给底层引擎）
func (cp *CPStorage) Scan(fn func(key string, val []byte)) {
	cp.engine.Scan(fn)
}

// Snapshot 序列化引擎当前状态（委托给底层引擎）
func (cp *CPStorage) Snapshot() ([]byte, error) {
	return cp.engine.Snapshot()
}

// Restore 从快照恢复引擎状态（委托给底层引擎）
func (cp *CPStorage) Restore(data []byte) error {
	return cp.engine.Restore(data)
}

// RaftStatus 返回 Raft 节点状态
func (cp *CPStorage) RaftStatus() raft.Status {
	return cp.node.Status()
}

// IsLeader 返回当前节点是否为 Leader
func (cp *CPStorage) IsLeader() bool {
	status := cp.node.Status()
	return status.Role == "Leader"
}

// ErrNotLeader 判断错误是否为非 Leader 错误
func IsNotLeader(err error) (string, bool) {
	var notLeader raft.ErrNotLeader
	if errors.As(err, &notLeader) {
		return notLeader.LeaderID, true
	}
	return "", false
}
