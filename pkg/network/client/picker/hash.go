package picker

import (
	"Flux-KV/pkg/consistent"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/metadata"
)

// HashPicker 使用一致性哈希选择节点（AP 模式）
// 支持双环（active + prev）以实现扩容期间的读回退
type HashPicker struct {
	activeRing *consistent.Map
	prevRing   *consistent.Map
	subConns   map[string]balancer.SubConn
	metrics    *RingMetrics
}

// SubConnInfo 包含 SubConn 及其地址
type SubConnInfo struct {
	SubConn balancer.SubConn
	Addr    string
}

// NewHashPicker 创建一致性哈希 Picker
// prevAddrs: 上一轮的节点地址列表，用于构建 prevRing；为空表示无旧环
// metrics: 环切换统计，用于衰减计数器提前丢弃旧环；可为 nil
func NewHashPicker(infos []SubConnInfo, prevAddrs []string, metrics *RingMetrics) *HashPicker {
	p := &HashPicker{
		activeRing: consistent.New(150, nil),
		subConns:   make(map[string]balancer.SubConn),
		metrics:    metrics,
	}
	for _, info := range infos {
		p.activeRing.Add(info.Addr)
		p.subConns[info.Addr] = info.SubConn
	}
	if len(prevAddrs) > 0 {
		p.prevRing = consistent.New(150, nil)
		for _, addr := range prevAddrs {
			// prevRing 中的节点可能已经被移除，如果 subConns 中没有，跳过
			if _, ok := p.subConns[addr]; ok {
				p.prevRing.Add(addr)
			}
		}
	}
	return p
}

// HasPrevRing 返回是否存在旧环
func (p *HashPicker) HasPrevRing() bool {
	return p.prevRing != nil
}

// ActiveNode 返回 key 在 activeRing 上映射的节点
func (p *HashPicker) ActiveNode(key string) string {
	return p.activeRing.Get(key)
}

// PrevNode 返回 key 在 prevRing 上映射的节点（无旧环时返回空）
func (p *HashPicker) PrevNode(key string) string {
	if p.prevRing == nil {
		return ""
	}
	return p.prevRing.Get(key)
}

// Pick 根据 key 的一致性哈希选择 SubConn
// 支持通过 metadata "x-flux-ring" = "prev" 强制使用旧环
func (p *HashPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	key := ""
	ringMode := "active"
	if md, ok := metadata.FromOutgoingContext(info.Ctx); ok {
		if vals := md.Get("x-flux-key"); len(vals) > 0 {
			key = vals[0]
		}
		if vals := md.Get("x-flux-ring"); len(vals) > 0 {
			ringMode = vals[0]
		}
	}

	var addr string
	if ringMode == "prev" && p.prevRing != nil {
		addr = p.prevRing.Get(key)
		if p.metrics != nil {
			p.metrics.RecordHit()
		}
	} else {
		addr = p.activeRing.Get(key)
	}

	if addr == "" {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}

	sc, ok := p.subConns[addr]
	if !ok {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}

	return balancer.PickResult{SubConn: sc}, nil
}
