package balancer

import (
	"context"
	"testing"

	"Flux-KV/pkg/network/client/picker"

	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"
)

// cpCtx 返回带有 x-flux-mode=cp 的 context
func cpCtx() context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-flux-mode", "cp"))
}

// mockSubConn 用于测试的 SubConn 占位符
type mockSubConn struct {
	balancer.SubConn
	id string
}

// buildPickerInfo 构造包含 CP/AP 节点的 PickerBuildInfo
func buildPickerInfo(cpNodes []struct {
	addr     string
	isLeader bool
}, apAddrs []string) base.PickerBuildInfo {
	ready := make(map[balancer.SubConn]base.SubConnInfo)

	for _, n := range cpNodes {
		attrs := attributes.New("mode", "cp").WithValue("is_leader", n.isLeader)
		sc := &mockSubConn{id: n.addr}
		ready[sc] = base.SubConnInfo{
			Address: resolver.Address{Addr: n.addr, Attributes: attrs},
		}
	}

	for _, addr := range apAddrs {
		attrs := attributes.New("mode", "ap")
		sc := &mockSubConn{id: addr}
		ready[sc] = base.SubConnInfo{
			Address: resolver.Address{Addr: addr, Attributes: attrs},
		}
	}

	return base.PickerBuildInfo{ReadySCs: ready}
}

// TestFluxPickerBuilder_CP_ReuseLeaderPicker 验证 CP 节点不变、leader 变化时复用 LeaderPicker
func TestFluxPickerBuilder_CP_ReuseLeaderPicker(t *testing.T) {
	builder := &fluxPickerBuilder{}

	// 第一次 Build：n1 是 leader
	info1 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", true},
		{"cp2:50052", false},
	}, []string{"ap1:50052", "ap2:50052"})

	picker1 := builder.Build(info1)
	fp1, ok := picker1.(*fluxPicker)
	if !ok {
		t.Fatal("expected *fluxPicker")
	}

	lp1, ok := fp1.cpPicker.(*picker.LeaderPicker)
	if !ok {
		t.Fatal("expected cpPicker to be *picker.LeaderPicker")
	}

	// 第二次 Build：CP 节点不变，但 leader 从 n1 切换到 n2
	info2 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", false},
		{"cp2:50052", true},
	}, []string{"ap1:50052", "ap2:50052"})

	picker2 := builder.Build(info2)
	fp2, ok := picker2.(*fluxPicker)
	if !ok {
		t.Fatal("expected *fluxPicker")
	}

	lp2, ok := fp2.cpPicker.(*picker.LeaderPicker)
	if !ok {
		t.Fatal("expected cpPicker to be *picker.LeaderPicker")
	}

	// 验证复用了同一个 LeaderPicker 实例
	if lp1 != lp2 {
		t.Fatal("expected LeaderPicker to be reused when CP addrs unchanged")
	}
}

// TestFluxPickerBuilder_CP_NewLeaderPickerOnAddrChange 验证 CP 节点变化时创建新 LeaderPicker
func TestFluxPickerBuilder_CP_NewLeaderPickerOnAddrChange(t *testing.T) {
	builder := &fluxPickerBuilder{}

	info1 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", true},
		{"cp2:50052", false},
	}, []string{"ap1:50052"})

	picker1 := builder.Build(info1)
	fp1 := picker1.(*fluxPicker)
	lp1 := fp1.cpPicker.(*picker.LeaderPicker)

	// CP 节点地址变化（新增 cp3）
	info2 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", true},
		{"cp2:50052", false},
		{"cp3:50052", false},
	}, []string{"ap1:50052"})

	picker2 := builder.Build(info2)
	fp2 := picker2.(*fluxPicker)
	lp2 := fp2.cpPicker.(*picker.LeaderPicker)

	if lp1 == lp2 {
		t.Fatal("expected new LeaderPicker when CP addrs changed")
	}
}

// TestFluxPickerBuilder_CP_LeaderThenNilThenNew 验证 leader -> nil -> new leader 的完整切换
func TestFluxPickerBuilder_CP_LeaderThenNilThenNew(t *testing.T) {
	builder := &fluxPickerBuilder{}

	// 初始状态：cp1 是 leader
	info1 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", true},
		{"cp2:50052", false},
	}, nil)

	p1 := builder.Build(info1)
	res1, _ := p1.Pick(balancer.PickInfo{Ctx: cpCtx()})
	if res1.SubConn == nil {
		t.Fatal("expected leader subconn on first build")
	}

	// leader 丢失（全部 is_leader=false）
	info2 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", false},
		{"cp2:50052", false},
	}, nil)

	p2 := builder.Build(info2)
	res2, _ := p2.Pick(balancer.PickInfo{Ctx: cpCtx()})
	if res2.SubConn == nil {
		t.Fatal("expected fallback to follower when leader is nil")
	}

	// 新的 leader 选出（cp2 成为 leader）
	info3 := buildPickerInfo([]struct {
		addr     string
		isLeader bool
	}{
		{"cp1:50052", false},
		{"cp2:50052", true},
	}, nil)

	p3 := builder.Build(info3)
	res3, _ := p3.Pick(balancer.PickInfo{Ctx: cpCtx()})
	if res3.SubConn == nil {
		t.Fatal("expected new leader subconn after leader election")
	}
}

// TestFluxPickerBuilder_AP_HashPickerAlwaysNew 验证 AP picker 每次 Build 都创建新的 HashPicker
func TestFluxPickerBuilder_AP_HashPickerAlwaysNew(t *testing.T) {
	builder := &fluxPickerBuilder{}

	info1 := buildPickerInfo(nil, []string{"ap1:50052", "ap2:50052"})
	p1 := builder.Build(info1)
	fp1 := p1.(*fluxPicker)

	// 相同的 AP 节点再次 Build
	info2 := buildPickerInfo(nil, []string{"ap1:50052", "ap2:50052"})
	p2 := builder.Build(info2)
	fp2 := p2.(*fluxPicker)

	// AP picker（HashPicker）每次 Build 都创建新的，不会复用
	if fp1.apPicker == fp2.apPicker {
		t.Fatal("expected HashPicker to be recreated on each Build")
	}
}
