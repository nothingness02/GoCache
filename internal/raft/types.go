package raft

import "sync"

// NodeState 定义 Raft 节点的状态
type NodeState int

const (
	Follower NodeState = iota
	Candidate
	Leader
)

func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// LogEntry 单条日志条目
type LogEntry struct {
	Index   uint64
	Term    uint64
	Command []byte // JSON 序列化的 Command
}

// Command 应用到状态机的命令
type Command struct {
	Op    string `json:"op"`    // "set" | "del"
	Key   string `json:"key"`
	Value string `json:"value"`
	TTL   int64  `json:"ttl"`   // 毫秒，0 表示无 TTL
}

// ApplyStorage 是 Raft 与应用层存储的接口
type ApplyStorage interface {
	ApplySet(key, value string, ttl int64)
	ApplyDelete(key string)
	Snapshot() ([]byte, error)
	Restore(data []byte) error
}

// Status 返回 Raft 节点的当前状态
type Status struct {
	Role         string
	Term         uint64
	CommitIndex  uint64
	LastApplied  uint64
	HealthyNodes int
	Peers        []string
}

// ErrNotLeader 表示当前节点不是 Leader
type ErrNotLeader struct {
	LeaderID string
}

func (e ErrNotLeader) Error() string {
	if e.LeaderID != "" {
		return "not leader, current leader: " + e.LeaderID
	}
	return "not leader"
}

// RaftNode Raft 节点核心结构
type RaftNode struct {
	mu sync.RWMutex

	// 持久化状态（需要持久化到磁盘）
	currentTerm uint64
	votedFor    string
	log         []LogEntry

	// 易失状态
	state       NodeState
	commitIndex uint64
	lastApplied uint64

	// Leader 特有状态
	nextIndex  map[string]uint64
	matchIndex map[string]uint64

	// 配置
	nodeID   string
	peers    []string
	bindAddr string
	dataDir  string

	// 外部依赖
	storage   ApplyStorage
	transport Transport
	wal       WAL

	// 快照状态
	lastSnapshotIndex uint64

	// 控制通道
	stopCh     chan struct{}
	appendCh   chan struct{} // 收到 AppendEntries 时触发
	electionCh chan struct{} // 选举超时触发
}
