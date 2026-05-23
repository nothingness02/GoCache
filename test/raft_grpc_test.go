package test

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	pb "Flux-KV/api/proto"
	"Flux-KV/internal/app"
	"Flux-KV/internal/raft"
	"Flux-KV/internal/storage"
	grpctransport "Flux-KV/internal/transport/grpc"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// cpNode 封装 CPStorage + 共享 gRPC 服务器，用于测试 gRPC 层的 Leader 发现
type cpNode struct {
	ID         string
	CPStore    *storage.CPStorage
	GRPCServer *grpc.Server
	Addr       string
}

// TestRaft_GRPC_LeaderDiscovery 测试通过 gRPC Status/RaftInfo 接口能否正确获取 Leader 信息
// 模拟 Gateway 查询后端 Raft 集群 Leader 的场景
func TestRaft_GRPC_LeaderDiscovery(t *testing.T) {
	peers := []string{"127.0.0.1:51131", "127.0.0.1:51132", "127.0.0.1:51133"}
	ids := []string{"n1", "n2", "n3"}

	nodes := make(map[string]*cpNode)
	for i, id := range ids {
		engine := &mockEngine{data: make(map[string]string)}
		raftCfg := &raft.Config{
			NodeID:   id,
			GroupID:  "test-grpc-group",
			Peers:    peers,
			BindAddr: peers[i],
			DataDir:  "",
		}
		cpStore, err := storage.NewCPStorage(engine, raftCfg)
		if err != nil {
			t.Fatalf("failed to create CPStorage for %s: %v", id, err)
		}

		// 启动共享 gRPC 服务器（KV + Raft 同端口）
		grpcServer := grpc.NewServer()
		meta := app.NodeMeta{NodeID: id, Mode: "cp"}
		uc := app.NewKVUseCase(cpStore, meta)
		pb.RegisterKVServiceServer(grpcServer, grpctransport.NewKVServer(uc))
		raft.RegisterRaftService(grpcServer, cpStore.Node())

		lis, err := net.Listen("tcp", peers[i])
		if err != nil {
			t.Fatalf("failed to listen on %s: %v", peers[i], err)
		}
		go func() {
			_ = grpcServer.Serve(lis)
		}()

		nodes[id] = &cpNode{
			ID:         id,
			CPStore:    cpStore,
			GRPCServer: grpcServer,
			Addr:       peers[i],
		}
	}

	// 等待 Leader 选出
	var leaderID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range ids {
			st := nodes[id].CPStore.RaftStatus()
			if st.Role == "Leader" {
				leaderID = id
				break
			}
		}
		if leaderID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if leaderID == "" {
		t.Fatal("timeout waiting for leader")
	}
	t.Logf("raft leader: %s", leaderID)

	// 模拟 Gateway 行为：轮询每个节点的 RaftInfo 接口，找出 Leader
	foundLeader := false
	leaderCount := 0
	maxTerm := uint64(0)

	for _, id := range ids {
		addr := nodes[id].Addr
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Logf("failed to connect to %s: %v", addr, err)
			continue
		}
		defer conn.Close()

		cli := pb.NewKVServiceClient(conn)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		resp, err := cli.Status(ctx2, &pb.StatusRequest{})
		cancel2()
		if err != nil {
			t.Logf("Status call to %s failed: %v", id, err)
			continue
		}

		ctx3, cancel3 := context.WithTimeout(context.Background(), 2*time.Second)
		raftResp, err := cli.RaftInfo(ctx3, &pb.RaftInfoRequest{})
		cancel3()
		if err != nil {
			t.Logf("RaftInfo call to %s failed: %v", id, err)
			continue
		}

		t.Logf("node %s: mode=%s role=%s term=%d commit=%d healthy=%v",
			id, resp.Mode, raftResp.Role, raftResp.Term, raftResp.CommitIndex, resp.Healthy)

		if raftResp.Role == "Leader" {
			foundLeader = true
			leaderCount++
		}
		if raftResp.Term > maxTerm {
			maxTerm = raftResp.Term
		}
	}

	if !foundLeader {
		t.Fatal("no leader found via gRPC Status/RaftInfo")
	}
	if leaderCount != 1 {
		t.Fatalf("expected 1 leader via gRPC, got %d", leaderCount)
	}
	if maxTerm == 0 {
		t.Fatal("leader term should be > 0")
	}

	// 停止 Leader，验证 gRPC 能发现新的 Leader
	t.Logf("stopping leader %s, checking failover via gRPC", leaderID)
	nodes[leaderID].CPStore.Close()
	nodes[leaderID].GRPCServer.Stop()

	// 等待新 Leader 选出
	newLeaderID := ""
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, id := range ids {
			if id == leaderID {
				continue
			}
			st := nodes[id].CPStore.RaftStatus()
			if st.Role == "Leader" {
				newLeaderID = id
				break
			}
		}
		if newLeaderID != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if newLeaderID == "" {
		t.Fatal("timeout waiting for new leader after stop")
	}

	// 重新查询存活节点
	foundLeader = false
	for _, id := range ids {
		if id == leaderID {
			continue // 已停止
		}
		addr := nodes[id].Addr
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			continue
		}
		defer conn.Close()

		cli := pb.NewKVServiceClient(conn)
		ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
		raftResp, err := cli.RaftInfo(ctx2, &pb.RaftInfoRequest{})
		cancel2()
		if err != nil {
			continue
		}
		if raftResp.Role == "Leader" {
			foundLeader = true
			t.Logf("new leader discovered via gRPC: %s (term=%d)", id, raftResp.Term)
		}
	}

	if !foundLeader {
		t.Fatal("no new leader found via gRPC after failover")
	}

	// 清理剩余节点
	for _, id := range ids {
		if id == leaderID {
			continue
		}
		nodes[id].CPStore.Close()
		nodes[id].GRPCServer.Stop()
	}
}

// mockEngine 是最小化的 Engine 实现，用于 gRPC 测试
// 仅提供基本的 Get/Set/Delete/Stats/Scan/Snapshot/Restore/Close 能力
type mockEngine struct {
	mu   sync.Mutex
	data map[string]string
}

func (e *mockEngine) Get(key string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	v, ok := e.data[key]
	return v, ok
}

func (e *mockEngine) Set(key string, value string, ttl time.Duration) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.data[key] = value
	return nil
}

func (e *mockEngine) Delete(key string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.data, key)
	return nil
}

func (e *mockEngine) Stats() storage.EngineStats {
	return storage.EngineStats{EngineType: "mock", EntryCount: int64(len(e.data))}
}

func (e *mockEngine) Scan(fn func(key string, val []byte)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range e.data {
		fn(k, []byte(v))
	}
}

func (e *mockEngine) Snapshot() ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.Marshal(e.data)
}

func (e *mockEngine) Restore(data []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.Unmarshal(data, &e.data)
}

func (e *mockEngine) Close() error {
	return nil
}
