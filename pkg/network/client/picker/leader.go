package picker

import (
	"google.golang.org/grpc/balancer"
)

// LeaderPicker 优先选择 Leader 节点（CP 模式）
type LeaderPicker struct {
	leader    balancer.SubConn
	followers []balancer.SubConn
}

// NewLeaderPicker 创建 Leader 优先 Picker
func NewLeaderPicker(infos []SubConnInfo, leader balancer.SubConn) *LeaderPicker {
	followers := make([]balancer.SubConn, 0, len(infos))
	for _, info := range infos {
		if leader != nil && info.SubConn == leader {
			continue
		}
		followers = append(followers, info.SubConn)
	}
	return &LeaderPicker{
		leader:    leader,
		followers: followers,
	}
}

// Pick 优先返回 Leader，Leader 不可用时随机选择 follower
func (p *LeaderPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	if p.leader != nil {
		return balancer.PickResult{SubConn: p.leader}, nil
	}

	if len(p.followers) > 0 {
		// 简单轮询：每次选择第一个可用的 follower
		// 更复杂的策略可以使用随机或轮询
		return balancer.PickResult{SubConn: p.followers[0]}, nil
	}

	return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
}
