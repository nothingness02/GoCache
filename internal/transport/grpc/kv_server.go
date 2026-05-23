package grpc

import (
	"context"

	pb "Flux-KV/api/proto"
	"Flux-KV/internal/app"
	"Flux-KV/pkg/logger"

	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

// KVServer 实现 pb.KVServiceServer 接口，作为 gRPC 传输适配层
type KVServer struct {
	pb.UnimplementedKVServiceServer
	uc app.KVUseCase
}

// NewKVServer 创建 KVServer 实例
func NewKVServer(uc app.KVUseCase) *KVServer {
	return &KVServer{uc: uc}
}

func extractRequestID(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-request-id"); len(vals) > 0 {
			return vals[0]
		}
	}
	return ""
}

func logFields(ctx context.Context, key string) []zap.Field {
	fields := []zap.Field{zap.String("key", key)}
	if reqID := extractRequestID(ctx); reqID != "" {
		fields = append(fields, zap.String("request_id", reqID))
	}
	return fields
}

// Set 处理 gRPC Set 请求
func (s *KVServer) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	if err := s.uc.Set(ctx, req.Key, req.Value); err != nil {
		logger.Log.Warn("[KVServer] Set failed", append(logFields(ctx, req.Key), zap.Error(err))...)
		return nil, err
	}
	logger.Log.Info("[KVServer] Set succeeded", logFields(ctx, req.Key)...)
	return &pb.SetResponse{Success: true}, nil
}

// Get 处理 gRPC Get 请求
func (s *KVServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	val, found, err := s.uc.Get(ctx, req.Key)
	if err != nil {
		logger.Log.Warn("[KVServer] Get failed", append(logFields(ctx, req.Key), zap.Error(err))...)
		return nil, err
	}
	logger.Log.Info("[KVServer] Get succeeded", append(logFields(ctx, req.Key), zap.Bool("found", found))...)
	return &pb.GetResponse{Value: val, Found: found}, nil
}

// Del 处理 gRPC Del 请求
func (s *KVServer) Del(ctx context.Context, req *pb.DelRequest) (*pb.DelResponse, error) {
	if err := s.uc.Del(ctx, req.Key); err != nil {
		logger.Log.Warn("[KVServer] Del failed", append(logFields(ctx, req.Key), zap.Error(err))...)
		return nil, err
	}
	logger.Log.Info("[KVServer] Del succeeded", logFields(ctx, req.Key)...)
	return &pb.DelResponse{Success: true}, nil
}

// InternalSet 处理节点间数据迁移写入（绕过 Raft）
func (s *KVServer) InternalSet(ctx context.Context, req *pb.InternalSetRequest) (*pb.InternalSetResponse, error) {
	if err := s.uc.InternalSet(ctx, req.Key, req.Value); err != nil {
		logger.Log.Warn("[KVServer] InternalSet failed", append(logFields(ctx, req.Key), zap.Error(err))...)
		return nil, err
	}
	logger.Log.Info("[KVServer] InternalSet succeeded", logFields(ctx, req.Key)...)
	return &pb.InternalSetResponse{Success: true}, nil
}

// Status 处理 gRPC Status 请求
func (s *KVServer) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	info, err := s.uc.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.StatusResponse{
		NodeId:     info.NodeID,
		Mode:       info.Mode,
		EngineType: info.EngineType,
		Stats: &pb.EngineStatsMsg{
			EngineType:  info.Stats.EngineType,
			EntryCount:  info.Stats.EntryCount,
			MemoryBytes: info.Stats.MemoryBytes,
		},
		Healthy: info.Healthy,
	}, nil
}

// RaftInfo 处理 gRPC RaftInfo 请求
func (s *KVServer) RaftInfo(ctx context.Context, req *pb.RaftInfoRequest) (*pb.RaftInfoResponse, error) {
	info, err := s.uc.RaftInfo(ctx)
	if err != nil {
		return nil, err
	}
	return &pb.RaftInfoResponse{
		Role:         info.Role,
		Term:         info.Term,
		CommitIndex:  info.CommitIndex,
		LastApplied:  info.LastApplied,
		HealthyNodes: info.HealthyNodes,
		Peers:        info.Peers,
	}, nil
}
