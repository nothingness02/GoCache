package raft

// Config Raft 节点配置
type Config struct {
	NodeID   string   // 当前节点唯一标识
	GroupID  string   // Raft 组 ID
	Peers    []string // 同组其他节点地址列表 (host:port)
	BindAddr string   // Raft RPC 监听地址
	DataDir  string   // Raft 日志和数据存储目录
}
