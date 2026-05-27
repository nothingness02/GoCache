package test

import (
	"math/rand"
	"testing"
	"time"

	"Flux-KV/internal/raft"
)

// TestRaft_OddNode_NormalOperation 测试 3 节点集群在持续负载下正常工作
func TestRaft_OddNode_NormalOperation(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51101", "127.0.0.1:51102", "127.0.0.1:51103"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51101+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	_, leaderWrap, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}
	t.Logf("leader elected: %s", leaderWrap.Node.Status().Role)

	// 持续提交多条命令，验证集群在高负载下稳定
	for i := 0; i < 20; i++ {
		cmd := raft.Command{Op: "set", Key: "key_" + string(rune('a'+i%26)), Value: string(rune('0' + i%10))}
		if ok, err := leaderWrap.Node.Propose(cmd); err != nil || !ok {
			t.Fatalf("failed to propose command %d: ok=%v err=%v", i, ok, err)
		}
	}

	// 等待所有命令被应用到所有节点
	// no-op at 1 + 20 commands = target index 21
	if !cluster.WaitForApplied(21, 5*time.Second) {
		t.Fatal("timeout waiting for all nodes to apply 20 commands")
	}
	pre_value := ""
	// 验证数据一致性
	for _, id := range []string{"n1", "n2", "n3"} {
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		value := wrap.Storage.Get("key_a")
		if value == "" {
			t.Fatalf("node %s should have key_a", id)
		}
		if pre_value == "" {
			pre_value = value
		} else {
			if pre_value != value {
				t.Fatalf("node %s should data not consistence ", id)
			}
		}
	}

	// 验证始终只有一个 Leader（无脑裂）
	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader after load, got %d", count)
	}

	// 验证所有节点 term 一致
	term := uint64(0)
	for _, id := range []string{"n1", "n2", "n3"} {
		st := cluster.GetNode(id).Node.Status()
		if term == 0 {
			term = st.Term
		} else if st.Term != term {
			t.Fatalf("node %s term %d != expected %d", id, st.Term, term)
		}
	}
	t.Logf("all nodes at term %d, commit index stable", term)
}

// TestRaft_OddNode_LoseFollower 测试 3 节点集群丢失一个 Follower 后仍能提交命令
func TestRaft_OddNode_LoseFollower(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51111", "127.0.0.1:51112", "127.0.0.1:51113"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51111+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}
	t.Logf("initial leader: %s", leaderID)

	// 停掉一个 Follower（非 Leader）
	var followerID string
	for _, id := range []string{"n1", "n2", "n3"} {
		if id != leaderID {
			followerID = id
			break
		}
	}
	cluster.StopNode(followerID)
	t.Logf("stopped follower %s", followerID)

	// 等待一下让集群感知到节点丢失
	time.Sleep(200 * time.Millisecond)

	// 验证 Leader 仍然是同一个（停掉 Follower 不应触发选举）
	leaderWrap := cluster.GetNode(leaderID)
	if leaderWrap.Node.Status().Role != "Leader" {
		t.Fatal("leader should remain leader after follower loss")
	}

	// 在剩余 2 节点中提交命令（Leader + 1 Follower = 2/3 = majority）
	cmd := raft.Command{Op: "set", Key: "after_follower_loss", Value: "yes"}
	if ok, err := leaderWrap.Node.Propose(cmd); err != nil || !ok {
		t.Fatalf("failed to propose after follower loss: ok=%v err=%v", ok, err)
	}

	// 等待命令提交
	time.Sleep(500 * time.Millisecond)

	// 验证 Leader 数据已更新
	if leaderWrap.Storage.Get("after_follower_loss") != "yes" {
		t.Fatal("leader should have applied after_follower_loss")
	}

	// 验证存活的 Follower 也同步了数据
	for _, id := range []string{"n1", "n2", "n3"} {
		if id == followerID || id == leaderID {
			continue
		}
		wrap := cluster.GetNode(id)
		if wrap.Storage.Get("after_follower_loss") != "yes" {
			t.Fatalf("alive follower %s should have after_follower_loss", id)
		}
	}

	// 此时停掉 Leader，验证剩余 1 节点无法选主（ minority）
	cluster.StopNode(leaderID)
	t.Logf("stopped leader %s, only 1 node left", leaderID)

	// 等待一段时间，确认无法选出 Leader
	time.Sleep(2 * time.Second)
	if count := cluster.LeaderCount(); count != 0 {
		t.Fatalf("expected 0 leader with minority (1/3), got %d", count)
	}
	t.Log("correctly: single node cannot elect leader")
}

// TestRaft_OddNode_RandomKill 测试随机时间关闭一个节点后集群恢复
func TestRaft_OddNode_RandomKill(t *testing.T) {
	cluster := NewRaftCluster()
	defer cluster.StopAll()

	peers := []string{"127.0.0.1:51121", "127.0.0.1:51122", "127.0.0.1:51123"}
	for i, id := range []string{"n1", "n2", "n3"} {
		if _, err := cluster.AddNode(id, 51121+i, peers); err != nil {
			t.Fatalf("failed to add node %s: %v", id, err)
		}
	}

	leaderID, leaderWrap, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader")
	}
	t.Logf("initial leader: %s", leaderID)

	// 先提交一些命令
	for i := 0; i < 5; i++ {
		cmd := raft.Command{Op: "set", Key: "init_" + string(rune('a'+i)), Value: "v" + string(rune('0'+i))}
		if ok, err := leaderWrap.Node.Propose(cmd); err != nil || !ok {
			t.Fatalf("failed to propose init command %d: ok=%v err=%v", i, ok, err)
		}
	}
	if !cluster.WaitForApplied(6, 3*time.Second) { // no-op + 5
		t.Fatal("timeout waiting for initial commands")
	}

	// 随机选择一个节点关闭（可能是 Leader 或 Follower）
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	nodeIDs := []string{"n1", "n2", "n3"}
	victimID := nodeIDs[r.Intn(len(nodeIDs))]
	isLeader := victimID == leaderID

	t.Logf("randomly killing %s (isLeader=%v)", victimID, isLeader)
	cluster.StopNode(victimID)

	// 等待集群恢复
	newLeader, _, ok := cluster.WaitForLeader(5 * time.Second)
	if !ok {
		t.Fatal("timeout waiting for leader after random kill")
	}
	if isLeader && newLeader == victimID {
		t.Fatal("new leader should be different from killed leader")
	}
	t.Logf("cluster recovered with new leader: %s", newLeader)

	// 验证只有一个 Leader
	if count := cluster.LeaderCount(); count != 1 {
		t.Fatalf("expected 1 leader after recovery, got %d", count)
	}

	// 在新 Leader 上提交命令，验证集群可用
	newLeaderWrap := cluster.GetNode(newLeader)
	cmd := raft.Command{Op: "set", Key: "after_random_kill", Value: "recovered"}
	if ok, err := newLeaderWrap.Node.Propose(cmd); err != nil || !ok {
		t.Fatalf("failed to propose after recovery: ok=%v err=%v", ok, err)
	}

	time.Sleep(500 * time.Millisecond)

	// 验证所有存活节点数据一致（不包括被关闭的节点）
	for _, id := range nodeIDs {
		if id == victimID {
			continue
		}
		wrap := cluster.GetNode(id)
		if wrap == nil {
			continue
		}
		if wrap.Storage.Get("after_random_kill") != "recovered" {
			t.Fatalf("node %s should have after_random_kill=recovered", id)
		}
		// 验证初始数据也保留了
		if wrap.Storage.Get("init_a") == "" {
			t.Fatalf("node %s should retain init_a", id)
		}
	}

	t.Log("random kill test passed: cluster recovered and data consistent")
}
