package grpc

import (
	"context"

	pb "Flux-KV/api/proto"
	"Flux-KV/internal/network/gateway"

	"google.golang.org/grpc/metadata"
)

// GatewayKVServer 是 Gateway 对外暴露的 gRPC 服务
// 接收外部 gRPC 请求，提取 metadata，通过内部 KVClient 转发到后端 Server
type GatewayKVServer struct {
	pb.UnimplementedKVServiceServer
	cli gateway.KVClient
}

// NewGatewayKVServer 创建 Gateway KV gRPC 服务
func NewGatewayKVServer(cli gateway.KVClient) *GatewayKVServer {
	return &GatewayKVServer{cli: cli}
}

// Set 处理写入请求
func (s *GatewayKVServer) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	mode := extractMode(ctx)
	if err := s.cli.SetWithMode(ctx, req.Key, req.Value, mode); err != nil {
		return &pb.SetResponse{Success: false}, err
	}
	return &pb.SetResponse{Success: true}, nil
}

// Get 处理读取请求
func (s *GatewayKVServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	mode := extractMode(ctx)
	val, err := s.cli.GetWithMode(ctx, req.Key, mode)
	if err != nil {
		return &pb.GetResponse{}, err
	}
	return &pb.GetResponse{Value: val}, nil
}

// Del 处理删除请求
func (s *GatewayKVServer) Del(ctx context.Context, req *pb.DelRequest) (*pb.DelResponse, error) {
	mode := extractMode(ctx)
	if err := s.cli.DelWithMode(ctx, req.Key, mode); err != nil {
		return &pb.DelResponse{Success: false}, err
	}
	return &pb.DelResponse{Success: true}, nil
}

// Status 返回 Gateway 状态（当前直接透传后端节点状态，取第一个可用节点）
// TODO: 未来可扩展为聚合多个后端节点的状态
func (s *GatewayKVServer) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	// Gateway 本身不持有存储，这里返回一个简化的 Gateway 状态
	// 实际节点状态应通过 admin HTTP 接口查询
	return &pb.StatusResponse{
		NodeId:  "gateway",
		Mode:    "gateway",
		Healthy: true,
	}, nil
}

// RaftInfo 返回 Raft 信息（Gateway 不直接参与 Raft，返回空）
func (s *GatewayKVServer) RaftInfo(ctx context.Context, req *pb.RaftInfoRequest) (*pb.RaftInfoResponse, error) {
	return &pb.RaftInfoResponse{}, nil
}

// extractMode 从 metadata 提取一致性模式，默认 ap
func extractMode(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-flux-mode"); len(vals) > 0 {
			if vals[0] == "cp" {
				return "cp"
			}
		}
	}
	return "ap"
}
