package raft

// RequestVoteArgs 投票请求参数
type RequestVoteArgs struct {
	Term         uint64
	CandidateID  string
	LastLogIndex uint64
	LastLogTerm  uint64
}

// RequestVoteReply 投票响应
type RequestVoteReply struct {
	Term        uint64
	VoteGranted bool
}

// AppendEntriesArgs 日志复制/心跳请求参数
type AppendEntriesArgs struct {
	Term         uint64
	LeaderID     string
	PrevLogIndex uint64
	PrevLogTerm  uint64
	Entries      []LogEntry
	LeaderCommit uint64
}

// AppendEntriesReply 日志复制/心跳响应
type AppendEntriesReply struct {
	Term    uint64
	Success bool
}

// Transport 定义 Raft 节点间通信的传输层接口
type Transport interface {
	// RequestVote 向指定节点发送投票请求
	RequestVote(peer string, args *RequestVoteArgs, reply *RequestVoteReply) error
	// AppendEntries 向指定节点发送日志复制/心跳请求
	AppendEntries(peer string, args *AppendEntriesArgs, reply *AppendEntriesReply) error
}
