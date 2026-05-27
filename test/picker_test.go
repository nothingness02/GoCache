package test

import (
	"context"
	"testing"

	"Flux-KV/pkg/network/client/picker"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/metadata"
)

// mockSubConn 是一个用于测试的 SubConn 占位符，不实现任何方法
// 通过不同的指针值来区分不同的 SubConn
type mockSubConn struct {
	balancer.SubConn
	id string
}

// ===== AP 模式 HashPicker 测试 =====

// TestPicker_AP_ScaleDown 验证缩容（3节点→2节点）后一致性哈希仍能正确路由
func TestPicker_AP_ScaleDown(t *testing.T) {
	// 旧环 3 节点，新环 2 节点（模拟 node3 被移除）
	infos := []picker.SubConnInfo{
		{SubConn: &mockSubConn{id: "n1"}, Addr: "node1:50052"},
		{SubConn: &mockSubConn{id: "n2"}, Addr: "node2:50052"},
	}
	prevAddrs := []string{"node1:50052", "node2:50052", "node3:50052"}

	hp := picker.NewHashPicker(infos, prevAddrs, nil)

	// 验证存在旧环
	if !hp.HasPrevRing() {
		t.Fatal("expected prev ring to exist after scale down")
	}

	// 验证 active ring 中所有 key 都映射到仍存在的节点，不会映射到 node3
	for i := 0; i < 100; i++ {
		key := "key-" + string(rune('a'+i%26))
		node := hp.ActiveNode(key)
		if node == "node3:50052" {
			t.Fatalf("active ring mapped key %s to removed node3", key)
		}
		if node != "node1:50052" && node != "node2:50052" {
			t.Fatalf("active ring mapped key %s to unexpected node %s", key, node)
		}
	}

	// 验证 prev ring 中 key 可以映射到旧节点（包括已被移除的 node3，
	// 但 prevRing 只包含仍在 subConns 中的节点，所以不会映射到 node3）
	prevNode := hp.PrevNode("some-key")
	if prevNode == "node3:50052" {
		t.Fatal("prev ring should not contain removed node3 if not in subConns")
	}

	// 验证通过 Pick + active ring 可以正确获取 SubConn
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-flux-key", "test-key"))
	res, err := hp.Pick(balancer.PickInfo{Ctx: ctx})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn == nil {
		t.Fatal("Pick returned nil SubConn")
	}
}

// TestPicker_AP_ScaleUp 验证扩容（2节点→3节点）后一致性哈希正确路由，且 prevRing 可回退
func TestPicker_AP_ScaleUp(t *testing.T) {
	// 旧环 2 节点，新环 3 节点（新增 node3）
	n1 := &mockSubConn{id: "n1"}
	n2 := &mockSubConn{id: "n2"}
	n3 := &mockSubConn{id: "n3"}

	infos := []picker.SubConnInfo{
		{SubConn: n1, Addr: "node1:50052"},
		{SubConn: n2, Addr: "node2:50052"},
		{SubConn: n3, Addr: "node3:50052"},
	}
	prevAddrs := []string{"node1:50052", "node2:50052"}

	metrics := picker.NewRingMetrics()
	hp := picker.NewHashPicker(infos, prevAddrs, metrics)

	// 验证存在旧环
	if !hp.HasPrevRing() {
		t.Fatal("expected prev ring to exist after scale up")
	}

	// 找到一个在旧环和新环中映射到不同节点的 key
	var diffKey string
	for i := 0; i < 1000; i++ {
		key := "key-" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		active := hp.ActiveNode(key)
		prev := hp.PrevNode(key)
		if active != "" && prev != "" && active != prev {
			diffKey = key
			t.Logf("found divergent key: %s -> active=%s prev=%s", key, active, prev)
			break
		}
	}
	if diffKey == "" {
		t.Log("no divergent key found in sample, but ring behavior is still valid")
	}

	// 验证 active ring 的 Pick 返回新环中的节点
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-flux-key", "test-key"))
	res, err := hp.Pick(balancer.PickInfo{Ctx: ctx})
	if err != nil {
		t.Fatalf("active ring Pick failed: %v", err)
	}
	if res.SubConn == nil {
		t.Fatal("active ring Pick returned nil SubConn")
	}

	// 验证 prev ring 的 Pick 可以回退到旧映射
	ctxPrev := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-flux-key", "test-key",
		"x-flux-ring", "prev",
	))
	resPrev, err := hp.Pick(balancer.PickInfo{Ctx: ctxPrev})
	if err != nil {
		t.Fatalf("prev ring Pick failed: %v", err)
	}
	if resPrev.SubConn == nil {
		t.Fatal("prev ring Pick returned nil SubConn")
	}

	// 验证 prev ring 命中被记录到 metrics
	if metrics.OldRingHits() != 1 {
		t.Fatalf("expected 1 old ring hit, got %v", metrics.OldRingHits())
	}
}

// TestPicker_AP_ScaleUp_KeyConsistency 验证扩容后相同 key 在新环中始终映射到同一节点
func TestPicker_AP_ScaleUp_KeyConsistency(t *testing.T) {
	infos := []picker.SubConnInfo{
		{SubConn: &mockSubConn{id: "n1"}, Addr: "node1:50052"},
		{SubConn: &mockSubConn{id: "n2"}, Addr: "node2:50052"},
		{SubConn: &mockSubConn{id: "n3"}, Addr: "node3:50052"},
	}
	hp := picker.NewHashPicker(infos, []string{"node1:50052", "node2:50052"}, nil)

	for i := 0; i < 100; i++ {
		key := "consistent-key"
		node := hp.ActiveNode(key)
		if node == "" {
			t.Fatal("active node should not be empty")
		}
		if i > 0 {
			prev := hp.ActiveNode(key)
			if prev != node {
				t.Fatalf("key %s mapped to different nodes: %s vs %s", key, prev, node)
			}
		}
	}
}

// ===== CP 模式 LeaderPicker 测试 =====

// TestPicker_CP_LeaderAvailable 验证 Leader 存在时优先返回 Leader
func TestPicker_CP_LeaderAvailable(t *testing.T) {
	leader := &mockSubConn{id: "leader"}
	follower1 := &mockSubConn{id: "f1"}
	follower2 := &mockSubConn{id: "f2"}

	infos := []picker.SubConnInfo{
		{SubConn: leader, Addr: "leader:50052"},
		{SubConn: follower1, Addr: "f1:50052"},
		{SubConn: follower2, Addr: "f2:50052"},
	}

	lp := picker.NewLeaderPicker(infos, leader)

	res, err := lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != leader {
		t.Fatal("expected Pick to return leader when leader is available")
	}
}

// TestPicker_CP_LeaderFailover 验证 Leader 丢失后回退到第一个 Follower
func TestPicker_CP_LeaderFailover(t *testing.T) {
	follower1 := &mockSubConn{id: "f1"}
	follower2 := &mockSubConn{id: "f2"}

	infos := []picker.SubConnInfo{
		{SubConn: follower1, Addr: "f1:50052"},
		{SubConn: follower2, Addr: "f2:50052"},
	}

	// Leader 为 nil（模拟 Leader 丢失/不可用）
	lp := picker.NewLeaderPicker(infos, nil)

	res, err := lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed after leader loss: %v", err)
	}
	if res.SubConn != follower1 {
		t.Fatal("expected Pick to fallback to first follower when leader is nil")
	}
}

// TestPicker_CP_LeaderFailover_OnlyFollower 验证只剩一个 Follower 时仍能服务
func TestPicker_CP_LeaderFailover_OnlyFollower(t *testing.T) {
	follower1 := &mockSubConn{id: "f1"}

	infos := []picker.SubConnInfo{
		{SubConn: follower1, Addr: "f1:50052"},
	}

	lp := picker.NewLeaderPicker(infos, nil)

	res, err := lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != follower1 {
		t.Fatal("expected Pick to return the only follower")
	}
}

// TestPicker_CP_NoNodesAvailable 验证无可用节点时返回错误
func TestPicker_CP_NoNodesAvailable(t *testing.T) {
	lp := picker.NewLeaderPicker([]picker.SubConnInfo{}, nil)

	_, err := lp.Pick(balancer.PickInfo{})
	if err == nil {
		t.Fatal("expected error when no nodes available")
	}
}

// TestPicker_CP_DynamicLeaderUpdate 验证 leader 动态更新后 Pick 返回新 leader
func TestPicker_CP_DynamicLeaderUpdate(t *testing.T) {
	oldLeader := &mockSubConn{id: "old-leader"}
	newLeader := &mockSubConn{id: "new-leader"}
	follower1 := &mockSubConn{id: "f1"}

	infos := []picker.SubConnInfo{
		{SubConn: oldLeader, Addr: "old:50052"},
		{SubConn: follower1, Addr: "f1:50052"},
	}

	lp := picker.NewLeaderPicker(infos, oldLeader)

	// 初始应返回 old leader
	res, err := lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != oldLeader {
		t.Fatal("expected initial leader to be oldLeader")
	}

	// 动态更新 leader
	lp.UpdateLeader(newLeader)

	// 更新后应返回 new leader
	res, err = lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed after update: %v", err)
	}
	if res.SubConn != newLeader {
		t.Fatal("expected Pick to return newLeader after UpdateLeader")
	}

	// leader 变为 nil 时回退到 follower
	lp.UpdateLeader(nil)
	res, err = lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed after nil leader: %v", err)
	}
	if res.SubConn != follower1 {
		t.Fatal("expected Pick to fallback to follower1 after leader set to nil")
	}
}

// TestPicker_CP_UpdateFollowers 验证 CP 节点增删时 UpdateFollowers 正确更新
func TestPicker_CP_UpdateFollowers(t *testing.T) {
	leader := &mockSubConn{id: "leader"}
	f1 := &mockSubConn{id: "f1"}
	f2 := &mockSubConn{id: "f2"}
	f3 := &mockSubConn{id: "f3"}

	lp := picker.NewLeaderPicker([]picker.SubConnInfo{
		{SubConn: leader, Addr: "leader:50052"},
		{SubConn: f1, Addr: "f1:50052"},
	}, leader)

	// 扩容：新增 f2, f3
	lp.UpdateFollowers([]picker.SubConnInfo{
		{SubConn: leader, Addr: "leader:50052"},
		{SubConn: f1, Addr: "f1:50052"},
		{SubConn: f2, Addr: "f2:50052"},
		{SubConn: f3, Addr: "f3:50052"},
	}, leader)

	res, err := lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != leader {
		t.Fatal("expected leader after UpdateFollowers")
	}

	// leader 切换 + 缩容
	newLeader := &mockSubConn{id: "new-leader"}
	lp.UpdateFollowers([]picker.SubConnInfo{
		{SubConn: newLeader, Addr: "new:50052"},
		{SubConn: f2, Addr: "f2:50052"},
	}, newLeader)

	res, err = lp.Pick(balancer.PickInfo{})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != newLeader {
		t.Fatal("expected newLeader after UpdateFollowers")
	}
}
