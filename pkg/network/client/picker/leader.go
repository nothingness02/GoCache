package picker

import (
	"sync"

	"google.golang.org/grpc/balancer"
)

// LeaderPicker 优先选择 Leader 节点（CP 模式）
// 支持动态更新 leader，当 resolver 报告 leader 变化时无需重建 picker
type LeaderPicker struct {
	mu        sync.RWMutex
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

// UpdateLeader 原子更新当前 leader SubConn（当 resolver 检测到 leader 切换时调用）
func (p *LeaderPicker) UpdateLeader(leader balancer.SubConn) {
	p.mu.Lock()
	p.leader = leader
	p.mu.Unlock()
}

// UpdateFollowers 更新 followers 列表（当 CP 节点增删时调用）
func (p *LeaderPicker) UpdateFollowers(infos []SubConnInfo, leader balancer.SubConn) {
	followers := make([]balancer.SubConn, 0, len(infos))
	for _, info := range infos {
		if leader != nil && info.SubConn == leader {
			continue
		}
		followers = append(followers, info.SubConn)
	}
	p.mu.Lock()
	p.leader = leader
	p.followers = followers
	p.mu.Unlock()
}

// Pick 优先返回 Leader，Leader 不可用时回退到第一个 follower
func (p *LeaderPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	p.mu.RLock()
	leader := p.leader
	followers := p.followers
	p.mu.RUnlock()

	if leader != nil {
		return balancer.PickResult{SubConn: leader}, nil
	}

	if len(followers) > 0 {
		return balancer.PickResult{SubConn: followers[0]}, nil
	}

	return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
}
