package discovery

import "encoding/json"

// NodeInfo 描述一个存储节点的注册信息
type NodeInfo struct {
	NodeID     string            `json:"node_id"`     // 节点唯一标识
	Addr       string            `json:"addr"`        // gRPC 服务地址
	GroupID    string            `json:"group_id"`    // 节点组 ID
	Mode       string            `json:"mode"`        // "cp" | "ap"
	IsLeader   bool              `json:"is_leader"`   // CP 模式下是否为 Leader
	EngineType string            `json:"engine_type"` // 底层引擎类型
	RaftAddr   string            `json:"raft_addr"`   // Raft RPC 地址（CP 模式）
	Metadata   map[string]string `json:"metadata"`    // 扩展字段
}

// ToJSON 序列化为 JSON 字符串
func (n *NodeInfo) ToJSON() (string, error) {
	b, err := json.Marshal(n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ParseNodeInfo 从 JSON 字符串解析 NodeInfo
func ParseNodeInfo(value string) (*NodeInfo, error) {
	var info NodeInfo
	if err := json.Unmarshal([]byte(value), &info); err != nil {
		return nil, err
	}
	return &info, nil
}
