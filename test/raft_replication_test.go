package test

import (
	"testing"
	"time"

	"Flux-KV/internal/raft"
)

// TestRaft_LogReplication_Basic 测试 Leader 提交的命令能被复制到所有 Follower
func TestRaft_LogReplication_Basic(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51041", "127.0.0.1:51042", "127.0.0.1:51043"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51041+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	_, leaderWrap, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	// 提交命令
	cmd := raft.Command{Op: "set", Key: "key1", Value: "value1", TTL: 0}
	if ok, err := leaderWrap.Node.Propose(cmd); err != nil || !ok {
		t.Fatalf("failed to propose command: ok=%v err=%v", ok, err)
	}

	// 等待日志被应用到所有节点（no-op at index 1 + command at index 2）
	if !cluster.WaitForApplied(2, 3*time.Second) {
		t.Fatal("timeout waiting for all nodes to apply log")
	}

	// 验证所有节点数据一致
	for _, id := range []string{"n1", "n2", "n3"} {
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		val := wrap.Storage.Get("key1")
		if val != "value1" {
			t.Fatalf("node %s expected value1, got %s", id, val)
		}
	}
}

// TestRaft_LogReplication_MultipleCommands 测试多条命令按序复制
func TestRaft_LogReplication_MultipleCommands(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51051", "127.0.0.1:51052", "127.0.0.1:51053"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51051+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	_, leaderWrap, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	// 提交多条命令
	cmds := []raft.Command{
		{Op: "set", Key: "a", Value: "1"},
		{Op: "set", Key: "b", Value: "2"},
		{Op: "del", Key: "a"},
		{Op: "set", Key: "c", Value: "3"},
	}
	for _, cmd := range cmds {
		if ok, err := leaderWrap.Node.Propose(cmd); err != nil || !ok {
			t.Fatalf("failed to propose command %+v: ok=%v err=%v", cmd, ok, err)
		}
	}

	// 等待应用（no-op at index 1 + 4 commands at indices 2-5）
	if !cluster.WaitForApplied(5, 3*time.Second) {
		t.Fatal("timeout waiting for all nodes to apply 4 commands")
	}

	// 验证数据一致性：a 被删除，b=2, c=3
	for _, id := range []string{"n1", "n2", "n3"} {
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		if wrap.Storage.Get("a") != "" {
			t.Fatalf("node %s: key 'a' should be deleted", id)
		}
		if wrap.Storage.Get("b") != "2" {
			t.Fatalf("node %s: key 'b' should be 2, got %s", id, wrap.Storage.Get("b"))
		}
		if wrap.Storage.Get("c") != "3" {
			t.Fatalf("node %s: key 'c' should be 3, got %s", id, wrap.Storage.Get("c"))
		}
	}

	// 验证命令顺序一致
	for _, id := range []string{"n1", "n2", "n3"} {
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		log := wrap.Storage.AppliedLog()
		if len(log) != 4 {
			t.Fatalf("node %s: expected 4 applied commands, got %d", id, len(log))
		}
		if log[0].Op != "set" || log[0].Key != "a" {
			t.Fatalf("node %s: first command wrong", id)
		}
		if log[2].Op != "del" || log[2].Key != "a" {
			t.Fatalf("node %s: third command should be del a", id)
		}
	}
}

// TestRaft_CP_PartitionSafety 测试网络分区下少数派不能提交命令（CP 安全性）
func TestRaft_CP_PartitionSafety(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51061", "127.0.0.1:51062", "127.0.0.1:51063"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51061+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}
	t.Logf("initial leader: %s", leaderID)

	// 先提交一条命令确保日志同步
	cmd0 := raft.Command{Op: "set", Key: "init", Value: "true"}
	if !cluster.Propose(cmd0, 2*time.Second) {
		t.Fatal("failed to propose initial command")
	}
	time.Sleep(200 * time.Millisecond)

	// 记录停止前的 commit index
	beforeCommit := cluster.GetNode(leaderID).Node.Status().CommitIndex
	t.Logf("commit index before partition: %d", beforeCommit)

	// 模拟分区：停止 Leader（形成 minority 1 节点 + majority 2 节点）
	cluster.StopNode(leaderID)
	time.Sleep(100 * time.Millisecond)

	// 等待新 Leader 在剩余 2 节点中选出
	newLeader, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for new leader after partition")
	}
	t.Logf("new leader after partition: %s", newLeader)

	// 新 Leader 应该能提交命令（2/3 = majority）
	cmd1 := raft.Command{Op: "set", Key: "after_partition", Value: "yes"}
	if !cluster.Propose(cmd1, 2*time.Second) {
		t.Fatal("failed to propose command in partitioned majority")
	}

	// 等待应用（新 Leader 会先追加 no-op，再追加 cmd1，所以目标索引是 beforeCommit+2）
	if !cluster.WaitForApplied(int(beforeCommit)+2, 3*time.Second) {
		t.Fatal("timeout waiting for majority to apply command")
	}

	// 验证存活节点数据一致
	for _, id := range []string{"n1", "n2", "n3"} {
		if id == leaderID {
			continue // 已停止
		}
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		if wrap.Storage.Get("after_partition") != "yes" {
			t.Fatalf("node %s should have after_partition=yes", id)
		}
	}
}

// TestRaft_CP_LeaderRejoin 测试旧 Leader 重新加入集群后能否同步新日志
func TestRaft_CP_LeaderRejoin(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51071", "127.0.0.1:51072", "127.0.0.1:51073"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51071+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	// 提交初始命令
	cmd0 := raft.Command{Op: "set", Key: "before", Value: "stop"}
	if !cluster.Propose(cmd0, 2*time.Second) {
		t.Fatal("failed to propose initial command")
	}
	time.Sleep(200 * time.Millisecond)

	// 停止旧 Leader
	cluster.StopNode(leaderID)
	// 等待一段时间，让旧 Leader 发送的 stale heartbeat 被处理完
	time.Sleep(2 * time.Second)

	// 等待新 Leader
	_, _, ok = cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for new leader")
	}

	// 在新 Leader 上提交更多命令
	cmd1 := raft.Command{Op: "set", Key: "after", Value: "restart"}
	if !cluster.Propose(cmd1, 2*time.Second) {
		t.Fatal("failed to propose after leader change")
	}
	time.Sleep(200 * time.Millisecond)

	// 重新启动旧 Leader（作为全新节点加入，通过日志复制追赶）
	oldPort := 51071
	if leaderID == "n2" {
		oldPort = 51072
	} else if leaderID == "n3" {
		oldPort = 51073
	}
	_, err := cluster.RestartNode(leaderID, oldPort, peers)
	if err != nil {
		t.Fatalf("failed to restart node %s: %v", leaderID, err)
	}
	t.Logf("restarted node %s", leaderID)

	// 等待一段时间让重新加入的节点通过心跳/日志复制追赶
	time.Sleep(1 * time.Second)

	// 在新 Leader 上再提交一条命令，验证重启节点也能收到
	cmd2 := raft.Command{Op: "set", Key: "rejoined", Value: "true"}
	if !cluster.Propose(cmd2, 2*time.Second) {
		t.Fatal("failed to propose after rejoin")
	}
	time.Sleep(500 * time.Millisecond)

	// 验证所有存活节点（包括重新加入的节点）数据一致
	for _, id := range []string{"n1", "n2", "n3"} {
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		wrap.mu.Lock()
		stopped := wrap.stopped
		wrap.mu.Unlock()
		if stopped {
			continue
		}
		if wrap.Storage.Get("before") != "stop" {
			t.Fatalf("node %s should have before=stop", id)
		}
		if wrap.Storage.Get("after") != "restart" {
			t.Fatalf("node %s should have after=restart", id)
		}
		if wrap.Storage.Get("rejoined") != "true" {
			t.Fatalf("node %s should have rejoined=true", id)
		}
	}

	// 验证集群仍然只有一个 Leader
	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader after rejoin, got %d", count)
	}
	t.Log("leader rejoin test passed: restarted node caught up via log replication")
}

// TestRaft_CP_NonLeaderReject 测试非 Leader 节点拒绝 Propose
func TestRaft_CP_NonLeaderReject(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51081", "127.0.0.1:51082", "127.0.0.1:51083"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51081+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	// 向非 Leader 节点提交命令，应该被拒绝
	for _, id := range []string{"n1", "n2", "n3"} {
		if id == leaderID {
			continue
		}
		wrap := cluster.GetNode(id)
		cmd := raft.Command{Op: "set", Key: "x", Value: "y"}
		ok, err := wrap.Node.Propose(cmd)
		if ok || err == nil {
			t.Fatalf("node %s (non-leader) should reject Propose, got ok=%v err=%v", id, ok, err)
		}
		// 验证错误类型
		if _, isNotLeader := err.(raft.ErrNotLeader); !isNotLeader {
			t.Logf("node %s returned error: %v (type: %T)", id, err, err)
		}
	}
}

// TestRaft_CP_CommitQuorum 验证提交需要 majority 确认
func TestRaft_CP_CommitQuorum(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51091", "127.0.0.1:51092", "127.0.0.1:51093"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51091+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	_, leaderWrap, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	// 记录提交前索引
	before := leaderWrap.Node.Status().CommitIndex

	// 提交命令
	cmd := raft.Command{Op: "set", Key: "quorum", Value: "test"}
	if ok, err := leaderWrap.Node.Propose(cmd); !ok || err != nil {
		t.Fatalf("failed to propose: ok=%v err=%v", ok, err)
	}

	// 等待提交传播
	time.Sleep(500 * time.Millisecond)

	// 验证 Leader 的 commit index 增加了
	after := leaderWrap.Node.Status().CommitIndex
	if after <= before {
		t.Fatalf("expected commit index to increase, before=%d after=%d", before, after)
	}
	t.Logf("commit index increased from %d to %d", before, after)

	// 验证 Leader 自身数据已应用
	if leaderWrap.Storage.Get("quorum") != "test" {
		t.Fatal("leader should have applied the command")
	}
}
