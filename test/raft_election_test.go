package test

import (
	"testing"
	"time"
)

// TestRaft_Election_SingleLeader 测试 3 节点集群能正确选举出一个 Leader
func TestRaft_Election_SingleLeader(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51001", "127.0.0.1:51002", "127.0.0.1:51003"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51001+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	// 等待选举出 Leader
	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader election")
	}
	t.Logf("elected leader: %s", leaderID)

	// 验证只有一个 Leader
	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader, got %d", count)
	}
}

// TestRaft_Election_LeaderFailover 测试 Leader 宕机后能否选出新 Leader
func TestRaft_Election_LeaderFailover(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51011", "127.0.0.1:51012", "127.0.0.1:51013"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51011+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for initial leader")
	}
	t.Logf("initial leader: %s", leaderID)

	// 停止 Leader
	cluster.StopNode(leaderID)
	t.Logf("stopped leader %s", leaderID)

	// 等待新 Leader
	newLeader, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for new leader after failover")
	}
	if newLeader == leaderID {
		t.Fatal("new leader should be different from stopped leader")
	}
	t.Logf("new leader after failover: %s", newLeader)

	// 验证只有一个 Leader
	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader after failover, got %d", count)
	}
}

// TestRaft_Election_TermMonotonic 验证 term 单调递增
func TestRaft_Election_TermMonotonic(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51021", "127.0.0.1:51022", "127.0.0.1:51023"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51021+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}

	initialTerm := cluster.GetNode(leaderID).Node.Status().Term
	t.Logf("initial leader term: %d", initialTerm)

	// 停止 Leader，触发重新选举
	cluster.StopNode(leaderID)
	newLeader, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for new leader")
	}

	newTerm := cluster.GetNode(newLeader).Node.Status().Term
	t.Logf("new leader term: %d", newTerm)

	if newTerm <= initialTerm {
		t.Fatalf("expected new term (%d) > initial term (%d)", newTerm, initialTerm)
	}
}

// TestRaft_Election_FiveNodes 测试 5 节点集群选举
func TestRaft_Election_FiveNodes(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{
		"127.0.0.1:51031", "127.0.0.1:51032", "127.0.0.1:51033",
		"127.0.0.1:51034", "127.0.0.1:51035",
	}
	for i, id := range []string{"n1", "n2", "n3", "n4", "n5"} {
		if _, err := cluster.AddNode(id, 51031+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader in 5-node cluster")
	}
	t.Logf("5-node cluster elected leader: %s", leaderID)

	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader in 5-node cluster, got %d", count)
	}
}
