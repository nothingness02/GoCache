package grpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	pb "Flux-KV/api/proto"
	"Flux-KV/internal/app"
	"Flux-KV/internal/config"
	"Flux-KV/internal/storage"
	grpctransport "Flux-KV/internal/transport/grpc"
	"Flux-KV/pkg/logger"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func init() {
	// 测试中初始化一个 Nop Logger，避免 nil pointer panic
	logger.Log = zap.NewNop()
}

// TestKVServerFlow 模拟启动 gRPC 服务器，通过客户端验证 Set/Get/Del 链路
func TestKVServerFlow(t *testing.T) {
	// 1. 启动服务端
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := lis.Addr().(*net.TCPAddr).Port
	t.Logf("Test Server started on port: %d", port)

	s := grpc.NewServer()
	db, err := storage.NewEngine(&config.Config{
		Storage: config.StorageConfig{
			ShardType:  "zerogc",
			LockerType: "sharded",
			ShardCount: 256,
		},
	})
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	uc := app.NewKVUseCase(db, app.NodeMeta{NodeID: "test-node", Mode: "ap"})
	pb.RegisterKVServiceServer(s, grpctransport.NewKVServer(uc))

	go func() {
		if err := s.Serve(lis); err != nil {
			t.Logf("Server finished with: %v", err)
		}
	}()
	defer s.Stop()

	time.Sleep(100 * time.Millisecond)

	// 2. 启动客户端
	addr := lis.Addr().String()
	ctxDial, cancelDial := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDial()
	conn, err := grpc.DialContext(ctxDial, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewKVServiceClient(conn)

	// 3. 开始测试
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// 3.1 测试 Set
	key, val := "test_key", "hello_grpc"
	_, err = client.Set(ctx, &pb.SetRequest{Key: key, Value: val})
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	t.Log("Set check passed")

	// 3.2 测试 Get
	getResp, err := client.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if getResp.Value != val {
		t.Errorf("Get value mismatch: got %v, want %v", getResp.Value, val)
	}
	t.Logf("Get check passed: %v", getResp.Value)

	// 3.3 测试 Del
	_, err = client.Del(ctx, &pb.DelRequest{Key: key})
	if err != nil {
		t.Fatalf("Del failed: %v", err)
	}
	t.Log("Del check passed")

	// 3.4 验证 Del 效果
	getRespAfterDel, err := client.Get(ctx, &pb.GetRequest{Key: key})
	if err != nil {
		t.Fatalf("Get after Del failed: %v", err)
	}
	if getRespAfterDel.Found {
		t.Errorf("Del not effective: key %s still exists", key)
	}
	t.Log("Del effect check passed: key not found")
}
