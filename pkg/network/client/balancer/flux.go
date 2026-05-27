package balancer

import (
	"sort"
	"sync"
	"time"

	"Flux-KV/pkg/network/client/picker"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/metadata"
)

// Name 是 Balancer 的名称
const Name = "flux"

// prevRingTTL 旧环保留时间，超过后不再回退
const prevRingTTL = 10 * time.Minute

func init() {
	balancer.Register(base.NewBalancerBuilder(Name, &fluxPickerBuilder{}, base.Config{HealthCheck: true}))
}

// Register 注册 Balancer（供外部调用）
func Register() {
	//init() //已自动注册，此处留作显式调用入口
}

type fluxPickerBuilder struct {
	mu              sync.Mutex
	lastActiveAddrs []string
	prevAddrs       []string
	prevRingAt      time.Time
	metrics         *picker.RingMetrics
	lastCpPicker    *picker.LeaderPicker
	lastCpAddrs     []string
	lastCpLeader    balancer.SubConn
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *fluxPickerBuilder) Build(info base.PickerBuildInfo) balancer.Picker {
	apInfos := make([]picker.SubConnInfo, 0)
	cpInfos := make([]picker.SubConnInfo, 0)
	var leader balancer.SubConn
	var currentAddrs []string
	var cpAddrs []string

	for sc, scInfo := range info.ReadySCs {
		addr := scInfo.Address.Addr
		mode := "ap"
		if scInfo.Address.Attributes != nil {
			if m, ok := scInfo.Address.Attributes.Value("mode").(string); ok {
				mode = m
			}
		}

		if mode == "cp" {
			cpInfos = append(cpInfos, picker.SubConnInfo{SubConn: sc, Addr: addr})
			cpAddrs = append(cpAddrs, addr)
			if scInfo.Address.Attributes != nil {
				if isLeader, ok := scInfo.Address.Attributes.Value("is_leader").(bool); ok && isLeader {
					leader = sc
				}
			}
		} else {
			apInfos = append(apInfos, picker.SubConnInfo{SubConn: sc, Addr: addr})
			currentAddrs = append(currentAddrs, addr)
		}
	}

	sort.Strings(currentAddrs)
	sort.Strings(cpAddrs)

	b.mu.Lock()
	if b.metrics == nil {
		b.metrics = picker.NewRingMetrics()
	}
	var prevAddrs []string
	if !stringSlicesEqual(b.lastActiveAddrs, currentAddrs) {
		// AP 节点列表发生变化，保存旧环并重置计数器
		prevAddrs = b.lastActiveAddrs
		b.prevAddrs = b.lastActiveAddrs
		b.prevRingAt = time.Now()
		b.metrics.Reset()
		b.lastActiveAddrs = currentAddrs
	} else if !b.prevRingAt.IsZero() {
		// 未变化，检查衰减计数器决定是否保留旧环
		if b.metrics.ShouldKeepPrevRing() {
			prevAddrs = b.prevAddrs
		} else {
			// 计数器连续低于阈值，提前丢弃旧环
			b.prevAddrs = nil
			b.prevRingAt = time.Time{}
		}
	}

	// CP 模式：如果节点地址没变且 leader 变了，复用 LeaderPicker 只更新 leader
	var cpPicker balancer.Picker
	if stringSlicesEqual(b.lastCpAddrs, cpAddrs) && b.lastCpPicker != nil {
		if leader != b.lastCpLeader {
			b.lastCpPicker.UpdateLeader(leader)
			b.lastCpLeader = leader
		}
		cpPicker = b.lastCpPicker
	} else {
		b.lastCpPicker = picker.NewLeaderPicker(cpInfos, leader)
		b.lastCpAddrs = cpAddrs
		b.lastCpLeader = leader
		cpPicker = b.lastCpPicker
	}
	b.mu.Unlock()

	return &fluxPicker{
		apPicker: picker.NewHashPicker(apInfos, prevAddrs, b.metrics),
		cpPicker: cpPicker,
	}
}

type fluxPicker struct {
	apPicker balancer.Picker
	cpPicker balancer.Picker
}

func (p *fluxPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	mode := "ap"
	if md, ok := metadata.FromOutgoingContext(info.Ctx); ok {
		if vals := md.Get("x-flux-mode"); len(vals) > 0 {
			mode = vals[0]
		}
	}

	if mode == "cp" {
		return p.cpPicker.Pick(info)
	}
	return p.apPicker.Pick(info)
}
