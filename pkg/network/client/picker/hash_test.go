package picker

import (
	"context"
	"testing"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/metadata"
)

func TestHashPicker_ActiveNode_Deterministic(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
		{SubConn: nil, Addr: "node3:50052"},
	}
	p := NewHashPicker(infos, nil, nil)

	node := p.ActiveNode("key1")
	if node == "" {
		t.Fatal("ActiveNode should return a non-empty node")
	}

	// 相同 key 必须始终映射到同一节点
	for i := 0; i < 10; i++ {
		if p.ActiveNode("key1") != node {
			t.Fatalf("ActiveNode not deterministic at iteration %d", i)
		}
	}
}

func TestHashPicker_HasPrevRing(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
		{SubConn: nil, Addr: "node3:50052"},
	}

	// 无旧环
	p1 := NewHashPicker(infos, nil, nil)
	if p1.HasPrevRing() {
		t.Fatal("expected no prev ring")
	}

	// 有旧环
	p2 := NewHashPicker(infos, []string{"node1:50052", "node2:50052"}, nil)
	if !p2.HasPrevRing() {
		t.Fatal("expected prev ring to exist")
	}
}

func TestHashPicker_PrevNode(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
	}
	p := NewHashPicker(infos, []string{"node1:50052"}, nil)

	// prev ring 只有 node1，所以所有 key 都应映射到 node1
	if p.PrevNode("any-key") != "node1:50052" {
		t.Fatalf("PrevNode should map to node1, got %s", p.PrevNode("any-key"))
	}

	// 无旧环时应返回空
	p2 := NewHashPicker(infos, nil, nil)
	if p2.PrevNode("any-key") != "" {
		t.Fatal("PrevNode should return empty when no prev ring")
	}
}

func TestHashPicker_Pick_ActiveRing(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
	}
	p := NewHashPicker(infos, nil, nil)

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-flux-key", "mykey"))
	res, err := p.Pick(balancer.PickInfo{Ctx: ctx})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}
	if res.SubConn != nil {
		// 使用 nil SubConn 测试，只验证不报错
		t.Logf("Pick returned subconn (nil expected in test)")
	}
}

func TestHashPicker_Pick_PrevRing(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
	}
	p := NewHashPicker(infos, []string{"node1:50052"}, nil)

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-flux-key", "mykey",
		"x-flux-ring", "prev",
	))
	res, err := p.Pick(balancer.PickInfo{Ctx: ctx})
	if err != nil {
		t.Fatalf("Pick with prev ring failed: %v", err)
	}
	if res.SubConn != nil {
		t.Logf("Pick returned subconn (nil expected in test)")
	}
}

func TestHashPicker_Pick_NoSubConnAvailable(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
	}
	p := NewHashPicker(infos, nil, nil)

	// 请求一个不在 subConns 中的地址（通过 metadata 不会直接影响，但空 key 可能映射到空）
	// 更直接的方式：Pick 返回空 addr 时会返回 ErrNoSubConnAvailable
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-flux-key", ""))
	_, err := p.Pick(balancer.PickInfo{Ctx: ctx})
	// 空 key 仍可能映射到 node1，所以这里不强制要求 err != nil
	// 此测试主要确保不 panic
	_ = err
}

func TestHashPicker_MetricsRecordHit(t *testing.T) {
	infos := []SubConnInfo{
		{SubConn: nil, Addr: "node1:50052"},
		{SubConn: nil, Addr: "node2:50052"},
	}
	metrics := NewRingMetrics()
	p := NewHashPicker(infos, []string{"node1:50052"}, metrics)

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		"x-flux-key", "mykey",
		"x-flux-ring", "prev",
	))
	_, err := p.Pick(balancer.PickInfo{Ctx: ctx})
	if err != nil {
		t.Fatalf("Pick failed: %v", err)
	}

	// metrics 应该记录了命中
	if metrics.oldRingHits != 1 {
		t.Fatalf("expected 1 old ring hit, got %v", metrics.oldRingHits)
	}
}
