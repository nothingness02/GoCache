package raft

import (
	"os"
	"path/filepath"
	"testing"
)

type testStorage struct {
	data map[string]string
}

func (t *testStorage) ApplySet(key, value string, ttl int64) { t.data[key] = value }
func (t *testStorage) ApplyDelete(key string)               { delete(t.data, key) }
func (t *testStorage) Snapshot() ([]byte, error)            { return []byte("snapdata"), nil }
func (t *testStorage) Restore(data []byte) error            { return nil }

// TestSnapshot_TruncateLog 验证 takeSnapshot 后内存 log 被截断，logOffset 正确更新
func TestSnapshot_TruncateLog(t *testing.T) {
	dir := t.TempDir()
	storage := &testStorage{data: make(map[string]string)}

	node, err := NewNode(&Config{
		NodeID:   "n1",
		GroupID:  "g1",
		Peers:    []string{"127.0.0.1:0"},
		BindAddr: "127.0.0.1:0",
		DataDir:  dir,
	}, storage)
	if err != nil {
		t.Fatal(err)
	}

	node.mu.Lock()
	node.log = []LogEntry{
		{Index: 1, Term: 1, Command: []byte(`{"op":"set","key":"a","value":"1"}`)},
		{Index: 2, Term: 1, Command: []byte(`{"op":"set","key":"b","value":"2"}`)},
		{Index: 3, Term: 1, Command: []byte(`{"op":"set","key":"c","value":"3"}`)},
		{Index: 4, Term: 1, Command: []byte(`{"op":"set","key":"d","value":"4"}`)},
		{Index: 5, Term: 1, Command: []byte(`{"op":"set","key":"e","value":"5"}`)},
	}
	node.currentTerm = 1
	node.lastApplied = 5
	node.mu.Unlock()

	if err := node.takeSnapshot(); err != nil {
		t.Fatalf("takeSnapshot failed: %v", err)
	}

	node.mu.RLock()
	if node.lastSnapshotIndex != 5 {
		t.Errorf("lastSnapshotIndex = %d, want 5", node.lastSnapshotIndex)
	}
	if node.lastSnapshotTerm != 1 {
		t.Errorf("lastSnapshotTerm = %d, want 1", node.lastSnapshotTerm)
	}
	if node.logOffset != 5 {
		t.Errorf("logOffset = %d, want 5", node.logOffset)
	}
	if len(node.log) != 0 {
		t.Errorf("log length = %d, want 0 after truncation", len(node.log))
	}
	if node.lastLogIndex() != 5 {
		t.Errorf("lastLogIndex = %d, want 5", node.lastLogIndex())
	}
	node.mu.RUnlock()

	if _, err := os.Stat(filepath.Join(dir, "snapshot-5.bin")); err != nil {
		t.Errorf("snapshot-5.bin not found: %v", err)
	}
}

// TestSnapshot_PartialTruncate 验证 lastApplied < lastLogIndex 时的部分截断
func TestSnapshot_PartialTruncate(t *testing.T) {
	dir := t.TempDir()
	storage := &testStorage{data: make(map[string]string)}

	node, err := NewNode(&Config{
		NodeID:   "n1",
		GroupID:  "g1",
		Peers:    []string{"127.0.0.1:0"},
		BindAddr: "127.0.0.1:0",
		DataDir:  dir,
	}, storage)
	if err != nil {
		t.Fatal(err)
	}

	node.mu.Lock()
	node.log = []LogEntry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
		{Index: 4, Term: 1},
		{Index: 5, Term: 1},
		{Index: 6, Term: 1},
		{Index: 7, Term: 1},
	}
	node.currentTerm = 1
	node.lastApplied = 5 // 只应用到 5，但日志到 7
	node.mu.Unlock()

	if err := node.takeSnapshot(); err != nil {
		t.Fatal(err)
	}

	node.mu.RLock()
	if node.logOffset != 5 {
		t.Errorf("logOffset = %d, want 5", node.logOffset)
	}
	if len(node.log) != 2 { // 保留 index 6,7
		t.Errorf("log length = %d, want 2", len(node.log))
	}
	if node.lastLogIndex() != 7 {
		t.Errorf("lastLogIndex = %d, want 7", node.lastLogIndex())
	}
	if node.log[0].Index != 6 {
		t.Errorf("log[0].Index = %d, want 6", node.log[0].Index)
	}
	node.mu.RUnlock()
}

// TestSnapshot_RestartRecovery 验证节点重启后从快照 + 截断 WAL 正确恢复
func TestSnapshot_RestartRecovery(t *testing.T) {
	dir := t.TempDir()
	storage := &testStorage{data: make(map[string]string)}

	cfg := &Config{
		NodeID:   "n1",
		GroupID:  "g1",
		Peers:    []string{"127.0.0.1:0"},
		BindAddr: "127.0.0.1:0",
		DataDir:  dir,
	}

	node, err := NewNode(cfg, storage)
	if err != nil {
		t.Fatal(err)
	}

	node.mu.Lock()
	node.log = []LogEntry{
		{Index: 1, Term: 1, Command: []byte(`{"op":"set","key":"a","value":"1"}`)},
		{Index: 2, Term: 1, Command: []byte(`{"op":"set","key":"b","value":"2"}`)},
		{Index: 3, Term: 1, Command: []byte(`{"op":"set","key":"c","value":"3"}`)},
	}
	node.currentTerm = 1
	node.lastApplied = 3
	node.mu.Unlock()

	if err := node.takeSnapshot(); err != nil {
		t.Fatal(err)
	}

	// 再添加新日志并保存 WAL
	node.mu.Lock()
	node.log = append(node.log, LogEntry{Index: 4, Term: 1, Command: []byte(`{"op":"set","key":"d","value":"4"}`)})
	node.saveState()
	node.mu.Unlock()

	// 模拟重启：重新创建节点
	node2, err := NewNode(cfg, storage)
	if err != nil {
		t.Fatal(err)
	}

	node2.mu.RLock()
	if node2.logOffset != 3 {
		t.Errorf("logOffset after restart = %d, want 3", node2.logOffset)
	}
	if len(node2.log) != 1 {
		t.Errorf("log length after restart = %d, want 1", len(node2.log))
	}
	if node2.lastLogIndex() != 4 {
		t.Errorf("lastLogIndex after restart = %d, want 4", node2.lastLogIndex())
	}
	if node2.lastSnapshotIndex != 3 {
		t.Errorf("lastSnapshotIndex after restart = %d, want 3", node2.lastSnapshotIndex)
	}
	if node2.lastSnapshotTerm != 1 {
		t.Errorf("lastSnapshotTerm after restart = %d, want 1", node2.lastSnapshotTerm)
	}
	node2.mu.RUnlock()
}

// TestSnapshot_HandleAppendEntries_CrossBoundary 验证截断后 handleAppendEntries 正确处理快照边界
func TestSnapshot_HandleAppendEntries_CrossBoundary(t *testing.T) {
	storage := &testStorage{data: make(map[string]string)}

	node, err := NewNode(&Config{
		NodeID:   "n1",
		GroupID:  "g1",
		Peers:    []string{"127.0.0.1:0"},
		BindAddr: "127.0.0.1:0",
		DataDir:  "",
	}, storage)
	if err != nil {
		t.Fatal(err)
	}

	// 模拟截断后的状态：快照到 index 3，内存保留 4,5
	node.mu.Lock()
	node.lastSnapshotIndex = 3
	node.lastSnapshotTerm = 1
	node.logOffset = 3
	node.log = []LogEntry{
		{Index: 4, Term: 1},
		{Index: 5, Term: 1},
	}
	node.currentTerm = 1
	node.mu.Unlock()

	// prevLogIndex == logOffset（快照边界，term 匹配，应成功）
	reply := &AppendEntriesReply{}
	node.handleAppendEntries(&AppendEntriesArgs{
		Term:         1,
		PrevLogIndex: 3,
		PrevLogTerm:  1,
		Entries: []LogEntry{
			{Index: 4, Term: 1, Command: []byte(`{"op":"set","key":"x","value":"y"}`)},
		},
		LeaderCommit: 4,
	}, reply)
	if !reply.Success {
		t.Error("expected success for prevLogIndex at snapshot boundary with matching term")
	}

	// prevLogIndex == logOffset（term 不匹配，应失败）
	reply = &AppendEntriesReply{}
	node.handleAppendEntries(&AppendEntriesArgs{
		Term:         1,
		PrevLogIndex: 3,
		PrevLogTerm:  2, // 不匹配的 term
		Entries:      []LogEntry{{Index: 4, Term: 1}},
	}, reply)
	if reply.Success {
		t.Error("expected failure for prevLogIndex at snapshot boundary with mismatch term")
	}

	// prevLogIndex < logOffset（太旧，应失败）
	reply = &AppendEntriesReply{}
	node.handleAppendEntries(&AppendEntriesArgs{
		Term:         1,
		PrevLogIndex: 2,
		PrevLogTerm:  1,
		Entries:      []LogEntry{{Index: 3, Term: 1}},
	}, reply)
	if reply.Success {
		t.Error("expected failure for prevLogIndex < logOffset")
	}

	// prevLogIndex > lastLogIndex（缺失，应失败）
	reply = &AppendEntriesReply{}
	node.handleAppendEntries(&AppendEntriesArgs{
		Term:         1,
		PrevLogIndex: 10,
		PrevLogTerm:  1,
	}, reply)
	if reply.Success {
		t.Error("expected failure for prevLogIndex > lastLogIndex")
	}
}

// TestSnapshot_ProposeAfterTruncate 验证截断后 Propose 新命令索引正确
func TestSnapshot_ProposeAfterTruncate(t *testing.T) {
	dir := t.TempDir()
	storage := &testStorage{data: make(map[string]string)}

	node, err := NewNode(&Config{
		NodeID:   "n1",
		GroupID:  "g1",
		Peers:    []string{"127.0.0.1:0"},
		BindAddr: "127.0.0.1:0",
		DataDir:  dir,
	}, storage)
	if err != nil {
		t.Fatal(err)
	}

	node.mu.Lock()
	node.state = Leader
	node.log = []LogEntry{
		{Index: 1, Term: 1},
		{Index: 2, Term: 1},
		{Index: 3, Term: 1},
	}
	node.currentTerm = 1
	node.lastApplied = 3
	node.mu.Unlock()

	if err := node.takeSnapshot(); err != nil {
		t.Fatal(err)
	}

	// 截断后 Propose 新命令
	ok, err := node.Propose(Command{Op: "set", Key: "k", Value: "v"})
	if !ok || err != nil {
		t.Fatalf("Propose after truncate failed: ok=%v err=%v", ok, err)
	}

	node.mu.RLock()
	if node.lastLogIndex() != 4 {
		t.Errorf("lastLogIndex = %d, want 4", node.lastLogIndex())
	}
	if len(node.log) != 1 {
		t.Errorf("log length = %d, want 1", len(node.log))
	}
	if node.log[0].Index != 4 {
		t.Errorf("log[0].Index = %d, want 4", node.log[0].Index)
	}
	node.mu.RUnlock()
}
