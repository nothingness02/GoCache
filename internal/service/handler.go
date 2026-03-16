package service

import (
	pb "Flux-KV/api/proto"
	"Flux-KV/internal/core"
	"Flux-KV/pkg/metrics"
	"context"
	"time"
)

// 定义服务结构体
type KVService struct {
	// gRPC 的保底实现
	pb.UnimplementedKVServiceServer

	// 持有内存数据库
	db *core.MemDB
}

// 构造函数
func NewKVService(db *core.MemDB) *KVService {
	return &KVService{
		db: db,
	}
}

// 下面是实现 .proto 里定义的三个接口

// 1. 实现 Set
func (s *KVService) Set(ctx context.Context, req *pb.SetRequest) (*pb.SetResponse, error) {
	// Good Practice: Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()
	// 核心逻辑：拿到请求里的 Key, Value，塞给数据库
	s.db.Set(req.Key, req.Value, 0)
	metrics.KVSetTotal.Inc() // 业务指标：SET 次数 +1
	metrics.GRPCRequestsTotal.WithLabelValues("Set", "request").Inc()
	metrics.HTTPRequestDuration.WithLabelValues("POST", "/kv", "200").Observe(time.Since(start).Seconds())
	return &pb.SetResponse{
		Success: true,
	}, nil
}

// 2. Get 接口
func (s *KVService) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	start := time.Now()
	// 核心逻辑：去数据库查
	val, found := s.db.Get(req.Key)
	if !found {
		metrics.KVGetTotal.Inc() // 业务指标：GET 次数 +1
		metrics.GRPCRequestsTotal.WithLabelValues("Get", "request").Inc()
		metrics.HTTPRequestDuration.WithLabelValues("GET", "/kv", "200").Observe(time.Since(start).Seconds())
		return &pb.GetResponse{
			Value: "",
			Found: false,
		}, nil
	}
	strVal, ok := val.(string)
	if !ok {
		strVal = ""
	}

	metrics.KVGetTotal.Inc() // 业务指标：GET 次数 +1
	metrics.GRPCRequestsTotal.WithLabelValues("Get", "request").Inc()
	metrics.HTTPRequestDuration.WithLabelValues("GET", "/kv", "200").Observe(time.Since(start).Seconds())

	return &pb.GetResponse{
		Value: strVal,
		Found: found,
	}, nil
}

// 3. Del 接口
func (s *KVService) Del(ctx context.Context, req *pb.DelRequest) (*pb.DelResponse, error) {
	s.db.Del(req.Key)
	return &pb.DelResponse{
		Success: true,
	}, nil
}
