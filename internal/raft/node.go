package raft

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

const (
	heartbeatInterval  = 100 * time.Millisecond
	minElectionTimeout = 500 * time.Millisecond
	maxElectionTimeout = 1000 * time.Millisecond
)

// randomTimeout 返回一个随机选举超时时间
func randomTimeout() time.Duration {
	return minElectionTimeout + time.Duration(rand.Int63n(int64(maxElectionTimeout-minElectionTimeout)))
}

// NewNode 创建一个新的 Raft 节点
func NewNode(cfg *Config, storage ApplyStorage) (*RaftNode, error) {
	n := &RaftNode{
		nodeID:     cfg.NodeID,
		peers:      cfg.Peers,
		bindAddr:   cfg.BindAddr,
		storage:    storage,
		transport:  NewGRPCTransport(),
		state:      Follower,
		log:        make([]LogEntry, 0),
		nextIndex:  make(map[string]uint64),
		matchIndex: make(map[string]uint64),
		stopCh:     make(chan struct{}),
		appendCh:   make(chan struct{}, 1),
		electionCh: make(chan struct{}, 1),
	}

	if cfg.DataDir != "" {
		n.dataDir = cfg.DataDir
		// 1. 先加载最新快照
		if _, meta, err := n.latestSnapshot(); err == nil && meta != nil {
			if err := n.storage.Restore(meta.Data); err != nil {
				return nil, fmt.Errorf("failed to restore snapshot: %w", err)
			}
			n.lastSnapshotIndex = meta.LastIncludedIndex
			n.lastApplied = meta.LastIncludedIndex
			log.Printf("[Raft] %s restored snapshot at index %d", cfg.NodeID, meta.LastIncludedIndex)
		}
		// 2. 再加载 WAL
		wal, err := NewFileWAL(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create WAL: %w", err)
		}
		n.wal = wal
		term, votedFor, entries, err := wal.LoadState()
		if err != nil {
			return nil, fmt.Errorf("failed to load WAL state: %w", err)
		}
		n.currentTerm = term
		n.votedFor = votedFor
		n.log = entries
		log.Printf("[Raft] %s loaded WAL state: term=%d, logEntries=%d", cfg.NodeID, term, len(entries))
	}

	return n, nil
}

// saveState 将持久化状态写入 WAL（调用方必须持有 n.mu 的写锁）
func (n *RaftNode) saveState() {
	if n.wal == nil {
		return
	}
	logCopy := make([]LogEntry, len(n.log))
	copy(logCopy, n.log)
	if err := n.wal.SaveState(n.currentTerm, n.votedFor, logCopy); err != nil {
		log.Printf("[Raft] WAL save failed: %v", err)
	}
}

// Start 启动 Raft 节点
func (n *RaftNode) Start() {
	go n.run()
	go n.applyLoop()
}

// Stop 停止 Raft 节点
func (n *RaftNode) Stop() {
	close(n.stopCh)
	if t, ok := n.transport.(*GRPCTransport); ok {
		t.Close()
	}
}

// Transport 返回当前传输层
func (n *RaftNode) Transport() Transport {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.transport
}

// ReadIndex 确保当前 Leader 的 commitIndex 追上最新日志，提供线性化读保证
func (n *RaftNode) ReadIndex(timeout time.Duration) error {
	n.mu.Lock()
	if n.state != Leader {
		n.mu.Unlock()
		return ErrNotLeader{}
	}
	// 追加一个 no-op 作为 read marker
	entry := LogEntry{
		Index: n.lastLogIndex() + 1,
		Term:  n.currentTerm,
	}
	n.log = append(n.log, entry)
	n.saveState()
	targetIndex := entry.Index
	n.mu.Unlock()

	// 触发日志复制
	go n.replicateLog()

	// 等待 commitIndex >= targetIndex
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n.mu.RLock()
		committed := n.commitIndex >= targetIndex
		isLeader := n.state == Leader
		n.mu.RUnlock()
		if !isLeader {
			return ErrNotLeader{}
		}
		if committed {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("read index timeout")
}

// Status 返回当前 Raft 状态
func (n *RaftNode) Status() Status {
	n.mu.RLock()
	defer n.mu.RUnlock()
	healthy := 1 // 自己总是健康的
	for _, peer := range n.peers {
		if peer == n.bindAddr {
			continue
		}
		healthy++
	}
	return Status{
		Role:         n.state.String(),
		Term:         n.currentTerm,
		CommitIndex:  n.commitIndex,
		LastApplied:  n.lastApplied,
		HealthyNodes: healthy,
		Peers:        n.peers,
	}
}

// Propose 向 Raft 提交一个命令，仅 Leader 可以提交
func (n *RaftNode) Propose(cmd Command) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != Leader {
		return false, ErrNotLeader{}
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return false, err
	}

	entry := LogEntry{
		Index:   n.lastLogIndex() + 1,
		Term:    n.currentTerm,
		Command: data,
	}
	n.log = append(n.log, entry)
	n.saveState()

	// Leader 立即尝试复制日志
	go n.replicateLog()

	return true, nil
}

// ===== 内部核心方法 =====

func (n *RaftNode) lastLogIndex() uint64 {
	if len(n.log) == 0 {
		return 0
	}
	return n.log[len(n.log)-1].Index
}

func (n *RaftNode) lastLogTerm() uint64 {
	if len(n.log) == 0 {
		return 0
	}
	return n.log[len(n.log)-1].Term
}

func (n *RaftNode) run() {
	for {
		select {
		case <-n.stopCh:
			return
		default:
		}

		n.mu.RLock()
		state := n.state
		n.mu.RUnlock()

		switch state {
		case Follower:
			n.runFollower()
		case Candidate:
			n.becomeCandidate()
		case Leader:
			n.runLeader()
		}
	}
}

func (n *RaftNode) drainAppendCh() {
	select {
	case <-n.appendCh:
	default:
	}
}

func (n *RaftNode) runFollower() {
	n.drainAppendCh()
	timer := time.NewTimer(randomTimeout())
	defer timer.Stop()

	for {
		select {
		case <-n.stopCh:
			return
		case <-n.appendCh:
			// 收到 AppendEntries，重置计时器
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(randomTimeout())
		case <-timer.C:
			// 选举超时，转为 Candidate 状态，由 run() 调用 becomeCandidate()
			n.mu.Lock()
			n.state = Candidate
			n.mu.Unlock()
			return
		}
	}
}

func (n *RaftNode) becomeCandidate() {
	n.drainAppendCh()

	n.mu.Lock()
	n.state = Candidate
	n.currentTerm++
	n.votedFor = n.nodeID
	n.saveState()
	term := n.currentTerm
	lastLogIndex := n.lastLogIndex()
	lastLogTerm := n.lastLogTerm()
	peers := make([]string, len(n.peers))
	copy(peers, n.peers)
	n.mu.Unlock()

	log.Printf("[Raft] %s became Candidate at term %d", n.nodeID, term)

	votes := 1 // 自己投自己
	var voteMu sync.Mutex
	majority := len(peers)/2 + 1

	doneCh := make(chan struct{})
	var doneOnce sync.Once

	// 向其他 peer 请求投票（自己已默认投自己）
	for _, peer := range peers {
		if peer == n.bindAddr {
			continue
		}
		go func(p string) {
			args := &RequestVoteArgs{
				Term:         term,
				CandidateID:  n.nodeID,
				LastLogIndex: lastLogIndex,
				LastLogTerm:  lastLogTerm,
			}
			reply := &RequestVoteReply{}
			if err := n.transport.RequestVote(p, args, reply); err != nil {
				return
			}

			if reply.VoteGranted {
				voteMu.Lock()
				votes++
				log.Printf("[Raft] %s received vote from %s, votes=%d/%d", n.nodeID, p, votes, majority)
				if votes >= majority {
					n.becomeLeader(term)
					doneOnce.Do(func() { close(doneCh) })
				}
				voteMu.Unlock()
			} else if reply.Term > term {
				// 发现更高的 term，退回 Follower
				n.mu.Lock()
				if reply.Term > n.currentTerm {
					n.currentTerm = reply.Term
					n.state = Follower
					n.votedFor = ""
				}
				n.mu.Unlock()
			}
		}(peer)
	}

	// 等待选举超时、收到 Leader 心跳或成为 Leader
	timer := time.NewTimer(randomTimeout())
	defer timer.Stop()
	select {
	case <-n.stopCh:
		return
	case <-n.appendCh:
		// 收到 Leader 心跳，退为 Follower
		n.mu.Lock()
		if n.state == Candidate {
			n.state = Follower
			n.votedFor = ""
		}
		n.mu.Unlock()
		return
	case <-doneCh:
		// 已收到多数票成为 Leader，立即返回以便开始发送心跳
		return
	case <-timer.C:
		// 选举超时，继续下一轮
	}
}

func (n *RaftNode) becomeLeader(term uint64) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != Candidate || n.currentTerm != term {
		return
	}

	n.state = Leader
	log.Printf("[Raft] %s became Leader at term %d", n.nodeID, n.currentTerm)

	// 初始化 Leader 状态
	for _, peer := range n.peers {
		n.nextIndex[peer] = n.lastLogIndex() + 1
		n.matchIndex[peer] = 0
	}

	// 追加 no-op 条目以快速推进 commitIndex
	// 只有当前 term 的日志条目才能直接提交，no-op 被多数复制后
	// tryCommit 会推进 commitIndex，从而间接提交之前 term 的所有已复制日志
	n.log = append(n.log, LogEntry{
		Index: n.lastLogIndex() + 1,
		Term:  n.currentTerm,
	})

	// 立即发送心跳（携带 no-op 条目）
	go n.sendHeartbeats()
}

func (n *RaftNode) runLeader() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-n.stopCh:
			return
		case <-ticker.C:
			n.mu.RLock()
			isLeader := n.state == Leader
			n.mu.RUnlock()
			if !isLeader {
				return
			}
			n.sendHeartbeats()
		}
	}
}

func (n *RaftNode) sendHeartbeats() {
	n.mu.RLock()
	if n.state != Leader {
		n.mu.RUnlock()
		return
	}
	term := n.currentTerm
	leaderID := n.nodeID
	commitIndex := n.commitIndex
	peers := make([]string, len(n.peers))
	copy(peers, n.peers)
	n.mu.RUnlock()

	for _, peer := range peers {
		if peer == n.bindAddr {
			continue
		}
		go func(p string) {
			n.mu.RLock()
			nextIdx := n.nextIndex[p]
			prevLogIndex := uint64(0)
			prevLogTerm := uint64(0)
			if nextIdx > 1 {
				prevLogIndex = nextIdx - 1
				if prevLogIndex <= uint64(len(n.log)) {
					prevLogTerm = n.log[prevLogIndex-1].Term
				}
			}
			var entries []LogEntry
			if nextIdx <= uint64(len(n.log)) {
				entries = n.log[nextIdx-1:]
			}
			n.mu.RUnlock()

			args := &AppendEntriesArgs{
				Term:         term,
				LeaderID:     leaderID,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      entries,
				LeaderCommit: commitIndex,
			}
			reply := &AppendEntriesReply{}
			if err := n.transport.AppendEntries(p, args, reply); err != nil {
				return
			}

			n.mu.Lock()
			defer n.mu.Unlock()

			if reply.Term > n.currentTerm {
				n.currentTerm = reply.Term
				n.state = Follower
				n.votedFor = ""
				return
			}

			if n.state != Leader {
				return
			}

			if reply.Success {
				if len(entries) > 0 {
					n.matchIndex[p] = entries[len(entries)-1].Index
					n.nextIndex[p] = n.matchIndex[p] + 1
				}
				// 检查是否可以更新 commitIndex
				n.tryCommit()
			} else {
				// 日志不匹配，回退 nextIndex
				// todo 结点自己返回nextIdx作为提示
				if n.nextIndex[p] > 1 {
					n.nextIndex[p]--
				}
			}
		}(peer)
	}
}

func (n *RaftNode) replicateLog() {
	n.sendHeartbeats()
}

func (n *RaftNode) tryCommit() {
	for idx := n.commitIndex + 1; idx <= n.lastLogIndex(); idx++ {
		if n.log[idx-1].Term != n.currentTerm {
			continue
		}
		count := 1 // 自己
		for _, peer := range n.peers {
			if peer == n.nodeID {
				continue
			}
			if n.matchIndex[peer] >= idx {
				count++
			}
		}
		if count > len(n.peers)/2 {
			n.commitIndex = idx
		} else {
			break
		}
	}
}

func (n *RaftNode) handleRequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.VoteGranted = false

	if args.Term < n.currentTerm {
		return
	}

	if args.Term > n.currentTerm {
		n.currentTerm = args.Term
		n.state = Follower
		n.votedFor = ""
		// 高 term 候选者出现，重置选举超时
		select {
		case n.appendCh <- struct{}{}:
		default:
		}
		n.saveState()
	}

	// 检查日志是否至少一样新
	lastLogIndex := n.lastLogIndex()
	lastLogTerm := n.lastLogTerm()
	if args.LastLogTerm < lastLogTerm {
		return
	}
	if args.LastLogTerm == lastLogTerm && args.LastLogIndex < lastLogIndex {
		return
	}

	if n.votedFor == "" || n.votedFor == args.CandidateID {
		n.votedFor = args.CandidateID
		reply.VoteGranted = true
		log.Printf("[Raft] %s voted for %s at term %d", n.nodeID, args.CandidateID, n.currentTerm)

		// 重置选举超时：投票给候选者后，自己不应再发起选举
		select {
		case n.appendCh <- struct{}{}:
		default:
		}
		n.saveState()
	}
}

func (n *RaftNode) handleAppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply.Term = n.currentTerm
	reply.Success = false

	if args.Term < n.currentTerm {
		return
	}

	// 收到合法 Leader 的心跳或日志复制
	if args.Term > n.currentTerm {
		n.currentTerm = args.Term
		n.votedFor = ""
		n.state = Follower
		n.saveState()
	} else if n.state == Candidate {
		n.state = Follower
	}

	// 通知 Follower 重置选举超时
	select {
	case n.appendCh <- struct{}{}:
	default:
	}

	// 检查 prevLogIndex 和 prevLogTerm 是否匹配
	if args.PrevLogIndex > 0 {
		if args.PrevLogIndex > uint64(len(n.log)) {
			return
		}
		if n.log[args.PrevLogIndex-1].Term != args.PrevLogTerm {
			return
		}
	}

	// 追加新日志条目
	if len(args.Entries) > 0 {
		// 截断不匹配的日志
		if args.PrevLogIndex < uint64(len(n.log)) {
			n.log = n.log[:args.PrevLogIndex]
		}
		n.log = append(n.log, args.Entries...)
		n.saveState()
	}

	// 更新 commitIndex
	if args.LeaderCommit > n.commitIndex {
		if args.LeaderCommit > uint64(len(n.log)) {
			n.commitIndex = uint64(len(n.log))
		} else {
			n.commitIndex = args.LeaderCommit
		}
	}

	reply.Success = true
}
