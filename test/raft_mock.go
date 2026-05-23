package test

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"Flux-KV/internal/raft"

	"google.golang.org/grpc"
)

// MockRaftStorage 记录 Raft 应用到状态机的命令
type MockRaftStorage struct {
	mu   sync.RWMutex
	data map[string]string
	log  []raft.Command
}

func NewMockRaftStorage() *MockRaftStorage {
	return &MockRaftStorage{
		data: make(map[string]string),
		log:  make([]raft.Command, 0),
	}
}

func (m *MockRaftStorage) ApplySet(key, value string, ttl int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	m.log = append(m.log, raft.Command{Op: "set", Key: key, Value: value, TTL: ttl})
}

func (m *MockRaftStorage) ApplyDelete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	m.log = append(m.log, raft.Command{Op: "del", Key: key})
}

func (m *MockRaftStorage) Get(key string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.data[key]
}

func (m *MockRaftStorage) AppliedCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.log)
}

func (m *MockRaftStorage) AppliedLog() []raft.Command {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]raft.Command, len(m.log))
	copy(out, m.log)
	return out
}

func (m *MockRaftStorage) Snapshot() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return json.Marshal(out)
}

func (m *MockRaftStorage) Restore(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var snap map[string]string
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	m.data = snap
	return nil
}

func (m *MockRaftStorage) Data() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]string, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

// RaftNodeWrap 包装 Raft 节点及其依赖
type RaftNodeWrap struct {
	Node      *raft.RaftNode
	Storage   *MockRaftStorage
	Server    *grpc.Server
	Transport *raft.GRPCTransport
	Addr      string
	stopped   bool
	mu        sync.Mutex
}

// RaftCluster 管理一组 Raft 节点
type RaftCluster struct {
	mu     sync.RWMutex
	nodes  map[string]*RaftNodeWrap
	baseIP string
}

func NewRaftCluster() *RaftCluster {
	return &RaftCluster{
		nodes:  make(map[string]*RaftNodeWrap),
		baseIP: "127.0.0.1",
	}
}

// AddNode 添加并启动一个 Raft 节点
func (c *RaftCluster) AddNode(nodeID string, port int, allPeers []string) (*RaftNodeWrap, error) {
	addr := c.addr(port)
	storage := NewMockRaftStorage()

	cfg := &raft.Config{
		NodeID:   nodeID,
		GroupID:  "test-group",
		Peers:    allPeers,
		BindAddr: addr,
		DataDir:  "",
	}

	node, err := raft.NewNode(cfg, storage)
	if err != nil {
		return nil, err
	}

	// 启动 gRPC 服务器
	transport := raft.NewGRPCTransport()
	if err := transport.StartServer(addr, node); err != nil {
		return nil, err
	}

	wrap := &RaftNodeWrap{
		Node:      node,
		Storage:   storage,
		Transport: transport,
		Addr:      addr,
	}

	c.mu.Lock()
	c.nodes[nodeID] = wrap
	c.mu.Unlock()

	node.Start()

	// 等待服务器就绪
	time.Sleep(50 * time.Millisecond)

	return wrap, nil
}

// WaitForLeader 等待集群选出 Leader，返回 Leader 的 nodeID 和 wrap
func (c *RaftCluster) WaitForLeader(timeout time.Duration) (string, *RaftNodeWrap, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.RLock()
		for id, wrap := range c.nodes {
			wrap.mu.Lock()
			stopped := wrap.stopped
			wrap.mu.Unlock()
			if stopped {
				continue
			}
			st := wrap.Node.Status()
			if st.Role == "Leader" {
				c.mu.RUnlock()
				return id, wrap, true
			}
		}
		c.mu.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}
	return "", nil, false
}

// LeaderCount 返回当前存活的 Leader 数量（用于检测脑裂）
func (c *RaftCluster) LeaderCount() int {
	count := 0
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, wrap := range c.nodes {
		wrap.mu.Lock()
		stopped := wrap.stopped
		wrap.mu.Unlock()
		if stopped {
			continue
		}
		if wrap.Node.Status().Role == "Leader" {
			count++
		}
	}
	return count
}

// GetNode 获取指定节点
func (c *RaftCluster) GetNode(id string) *RaftNodeWrap {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodes[id]
}

// StopNode 停止指定节点
func (c *RaftCluster) StopNode(id string) {
	c.mu.RLock()
	wrap, ok := c.nodes[id]
	c.mu.RUnlock()
	if !ok {
		return
	}
	wrap.mu.Lock()
	if wrap.stopped {
		wrap.mu.Unlock()
		return
	}
	wrap.stopped = true
	wrap.mu.Unlock()
	wrap.Node.Stop()
	wrap.Transport.Close()
}

// RestartNode 重新启动已停止的节点（用于 LeaderRejoin 测试）
func (c *RaftCluster) RestartNode(nodeID string, port int, allPeers []string) (*RaftNodeWrap, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清理旧节点引用（已 Stop 过，无需重复 Stop）
	delete(c.nodes, nodeID)

	addr := c.addr(port)
	storage := NewMockRaftStorage()

	cfg := &raft.Config{
		NodeID:   nodeID,
		GroupID:  "test-group",
		Peers:    allPeers,
		BindAddr: addr,
		DataDir:  "",
	}

	node, err := raft.NewNode(cfg, storage)
	if err != nil {
		return nil, err
	}

	transport := raft.NewGRPCTransport()
	if err := transport.StartServer(addr, node); err != nil {
		return nil, err
	}

	wrap := &RaftNodeWrap{
		Node:      node,
		Storage:   storage,
		Transport: transport,
		Addr:      addr,
		stopped:   false,
	}

	c.nodes[nodeID] = wrap
	node.Start()
	time.Sleep(50 * time.Millisecond)

	return wrap, nil
}

// StopAll 停止所有节点
func (c *RaftCluster) StopAll() {
	c.mu.RLock()
	ids := make([]string, 0, len(c.nodes))
	for id := range c.nodes {
		ids = append(ids, id)
	}
	c.mu.RUnlock()
	for _, id := range ids {
		c.StopNode(id)
	}
}

// CountCommitted 返回所有节点中已提交的最小日志索引
func (c *RaftCluster) MinCommitIndex() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var min uint64 = ^uint64(0)
	for _, wrap := range c.nodes {
		st := wrap.Node.Status()
		if st.CommitIndex < min {
			min = st.CommitIndex
		}
	}
	return min
}

func (c *RaftCluster) addr(port int) string {
	return fmt.Sprintf("%s:%d", c.baseIP, port)
}

// WaitForApplied 等待所有存活的节点应用日志到指定索引
func (c *RaftCluster) WaitForApplied(minApplied int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		allOk := true
		c.mu.RLock()
		for _, wrap := range c.nodes {
			wrap.mu.Lock()
			stopped := wrap.stopped
			wrap.mu.Unlock()
			if stopped {
				continue
			}
			if int(wrap.Node.Status().LastApplied) < minApplied {
				allOk = false
				break
			}
		}
		c.mu.RUnlock()
		if allOk {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// Propose 向 Leader 提交命令，重试直到成功
func (c *RaftCluster) Propose(cmd raft.Command, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.RLock()
		for _, wrap := range c.nodes {
			wrap.mu.Lock()
			stopped := wrap.stopped
			wrap.mu.Unlock()
			if stopped {
				continue
			}
			_, err := wrap.Node.Propose(cmd)
			if err == nil {
				c.mu.RUnlock()
				return true
			}
		}
		c.mu.RUnlock()
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// CountCommitted 返回已提交到指定索引的存活节点数量
func (c *RaftCluster) CountCommitted(index uint64) int {
	count := 0
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, wrap := range c.nodes {
		wrap.mu.Lock()
		stopped := wrap.stopped
		wrap.mu.Unlock()
		if stopped {
			continue
		}
		if wrap.Node.Status().CommitIndex >= index {
			count++
		}
	}
	return count
}

// Partition 模拟网络分区，将节点分为两个组（majority 和 minority）
// majority 中的节点能看到彼此，minority 被隔离
func (c *RaftCluster) Partition(majorityIDs, minorityIDs []string) {
	// 当前实现基于真实的 gRPC transport，无法简单做网络分区
	// 我们通过停止 minority 节点来模拟分区
	for _, id := range minorityIDs {
		c.StopNode(id)
	}
}
