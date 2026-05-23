package raft

import (
	raftpb "Flux-KV/api/proto/raft"
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ===== 类型转换函数 =====

func toProtoLogEntries(entries []LogEntry) []*raftpb.LogEntry {
	result := make([]*raftpb.LogEntry, len(entries))
	for i, e := range entries {
		result[i] = &raftpb.LogEntry{
			Index:   e.Index,
			Term:    e.Term,
			Command: e.Command,
		}
	}
	return result
}

func fromProtoLogEntries(entries []*raftpb.LogEntry) []LogEntry {
	result := make([]LogEntry, len(entries))
	for i, e := range entries {
		result[i] = LogEntry{
			Index:   e.Index,
			Term:    e.Term,
			Command: e.Command,
		}
	}
	return result
}

func toProtoRequestVoteArgs(args *RequestVoteArgs) *raftpb.RequestVoteRequest {
	return &raftpb.RequestVoteRequest{
		Term:         args.Term,
		CandidateId:  args.CandidateID,
		LastLogIndex: args.LastLogIndex,
		LastLogTerm:  args.LastLogTerm,
	}
}

func fromProtoRequestVoteReply(reply *raftpb.RequestVoteResponse) *RequestVoteReply {
	return &RequestVoteReply{
		Term:        reply.Term,
		VoteGranted: reply.VoteGranted,
	}
}

func toProtoAppendEntriesArgs(args *AppendEntriesArgs) *raftpb.AppendEntriesRequest {
	return &raftpb.AppendEntriesRequest{
		Term:         args.Term,
		LeaderId:     args.LeaderID,
		PrevLogIndex: args.PrevLogIndex,
		PrevLogTerm:  args.PrevLogTerm,
		Entries:      toProtoLogEntries(args.Entries),
		LeaderCommit: args.LeaderCommit,
	}
}

func fromProtoAppendEntriesReply(reply *raftpb.AppendEntriesResponse) *AppendEntriesReply {
	return &AppendEntriesReply{
		Term:    reply.Term,
		Success: reply.Success,
	}
}

// ===== GRPCTransport =====

// GRPCTransport 基于 gRPC 的 Raft 传输层实现
type GRPCTransport struct {
	mu         sync.RWMutex
	clients    map[string]raftpb.RaftServiceClient
	conns      map[string]*grpc.ClientConn
	grpcServer *grpc.Server
}

// NewGRPCTransport 创建 gRPC 传输层
func NewGRPCTransport() *GRPCTransport {
	return &GRPCTransport{
		clients: make(map[string]raftpb.RaftServiceClient),
		conns:   make(map[string]*grpc.ClientConn),
	}
}

// getClient 获取或创建到指定 peer 的客户端
func (t *GRPCTransport) getClient(peer string) (raftpb.RaftServiceClient, error) {
	t.mu.RLock()
	client, ok := t.clients[peer]
	t.mu.RUnlock()
	if ok {
		return client, nil
	}

	// 在锁外执行网络连接，避免阻塞其他 goroutine
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, peer,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to peer %s: %w", peer, err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// 双重检查：其他 goroutine 可能已创建连接
	if existing, ok := t.clients[peer]; ok {
		conn.Close()
		return existing, nil
	}

	client = raftpb.NewRaftServiceClient(conn)
	t.clients[peer] = client
	t.conns[peer] = conn
	return client, nil
}

// removeClient 从缓存中移除指定 peer 的连接
func (t *GRPCTransport) removeClient(peer string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if conn, ok := t.conns[peer]; ok {
		conn.Close()
		delete(t.conns, peer)
		delete(t.clients, peer)
	}
}

// RequestVote 向指定节点发送投票请求
func (t *GRPCTransport) RequestVote(peer string, args *RequestVoteArgs, reply *RequestVoteReply) error {
	client, err := t.getClient(peer)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.RequestVote(ctx, toProtoRequestVoteArgs(args))
	if err != nil {
		t.removeClient(peer)
		return err
	}

	*reply = *fromProtoRequestVoteReply(resp)
	return nil
}

// AppendEntries 向指定节点发送日志复制/心跳请求
func (t *GRPCTransport) AppendEntries(peer string, args *AppendEntriesArgs, reply *AppendEntriesReply) error {
	client, err := t.getClient(peer)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.AppendEntries(ctx, toProtoAppendEntriesArgs(args))
	if err != nil {
		return err
	}

	*reply = *fromProtoAppendEntriesReply(resp)
	return nil
}

// Close 关闭所有连接和 gRPC 服务器
func (t *GRPCTransport) Close() {
	t.mu.Lock()
	for addr, conn := range t.conns {
		conn.Close()
		delete(t.conns, addr)
		delete(t.clients, addr)
	}
	server := t.grpcServer
	t.grpcServer = nil
	t.mu.Unlock()

	if server != nil {
		server.Stop()
	}
}

// RegisterRaftService 将 Raft 服务注册到已有的 gRPC 服务器
func RegisterRaftService(s *grpc.Server, node *RaftNode) {
	raftpb.RegisterRaftServiceServer(s, &RaftGRPCServer{node: node})
}

// StartServer 启动独立的 gRPC 服务器监听 Raft RPC（测试兼容）
func (t *GRPCTransport) StartServer(bindAddr string, node *RaftNode) error {
	lis, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("failed to listen raft rpc on %s: %w", bindAddr, err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.grpcServer != nil {
		return fmt.Errorf("raft RPC server already started")
	}

	grpcServer := grpc.NewServer()
	raftpb.RegisterRaftServiceServer(grpcServer, &RaftGRPCServer{node: node})
	t.grpcServer = grpcServer

	go func() {
		log.Printf("🚀 Raft RPC server listening on %s", bindAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("Raft RPC server stopped: %v", err)
		}
	}()

	return nil
}

// ===== RaftGRPCServer =====

// RaftGRPCServer 实现 raftpb.RaftServiceServer 接口
type RaftGRPCServer struct {
	raftpb.UnimplementedRaftServiceServer
	node *RaftNode
}

// RequestVote 处理投票请求
func (s *RaftGRPCServer) RequestVote(ctx context.Context, req *raftpb.RequestVoteRequest) (*raftpb.RequestVoteResponse, error) {
	args := &RequestVoteArgs{
		Term:         req.Term,
		CandidateID:  req.CandidateId,
		LastLogIndex: req.LastLogIndex,
		LastLogTerm:  req.LastLogTerm,
	}
	reply := &RequestVoteReply{}
	s.node.handleRequestVote(args, reply)
	return &raftpb.RequestVoteResponse{
		Term:        reply.Term,
		VoteGranted: reply.VoteGranted,
	}, nil
}

// AppendEntries 处理日志复制/心跳请求
func (s *RaftGRPCServer) AppendEntries(ctx context.Context, req *raftpb.AppendEntriesRequest) (*raftpb.AppendEntriesResponse, error) {
	args := &AppendEntriesArgs{
		Term:         req.Term,
		LeaderID:     req.LeaderId,
		PrevLogIndex: req.PrevLogIndex,
		PrevLogTerm:  req.PrevLogTerm,
		Entries:      fromProtoLogEntries(req.Entries),
		LeaderCommit: req.LeaderCommit,
	}
	reply := &AppendEntriesReply{}
	s.node.handleAppendEntries(args, reply)
	return &raftpb.AppendEntriesResponse{
		Term:    reply.Term,
		Success: reply.Success,
	}, nil
}
