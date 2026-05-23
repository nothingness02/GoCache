package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	pb "Flux-KV/api/proto"
	"Flux-KV/pkg/network/discovery"
	"Flux-KV/pkg/resilience"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Handler 处理管理接口请求
type Handler struct {
	disco        *discovery.Discovery
	rateLimiter  resilience.RateLimiter
	circuitBreaker resilience.CircuitBreaker
}

func NewHandler(disco *discovery.Discovery, rl resilience.RateLimiter, cb resilience.CircuitBreaker) *Handler {
	return &Handler{
		disco:          disco,
		rateLimiter:    rl,
		circuitBreaker: cb,
	}
}

// ServeHTTP 路由 admin 请求
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/admin/nodes":
		h.ListNodes(w, r)
	case strings.HasPrefix(path, "/admin/nodes/") && strings.HasSuffix(path, "/status"):
		// /admin/nodes/:addr/status
		addr := strings.TrimPrefix(path, "/admin/nodes/")
		addr = strings.TrimSuffix(addr, "/status")
		h.NodeStatus(w, r, addr)
	case path == "/admin/stats":
		h.ClusterStats(w, r)
	case path == "/admin/resilience":
		h.ResilienceStatus(w, r)
	default:
		http.NotFound(w, r)
	}
}

// ListNodes 列出所有注册节点
// GET /admin/nodes
func (h *Handler) ListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.disco.ListNodes("/services/kv-service/")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
}

// NodeStatus 查询指定节点的状态
// GET /admin/nodes/:addr/status
func (h *Handler) NodeStatus(w http.ResponseWriter, r *http.Request, addr string) {
	if addr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing addr"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "connect failed: " + err.Error()})
		return
	}
	defer conn.Close()

	client := pb.NewKVServiceClient(conn)
	statusCtx, statusCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer statusCancel()

	statusResp, err := client.Status(statusCtx, &pb.StatusRequest{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "status failed: " + err.Error()})
		return
	}

	raftCtx, raftCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer raftCancel()
	raftResp, _ := client.RaftInfo(raftCtx, &pb.RaftInfoRequest{})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"addr":      addr,
		"status":    statusResp,
		"raft_info": raftResp,
	})
}

// ClusterStats 汇总集群统计信息
// GET /admin/stats
func (h *Handler) ClusterStats(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.disco.ListNodes("/services/kv-service/")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type nodeResult struct {
		Addr   string            `json:"addr"`
		Status *pb.StatusResponse `json:"status,omitempty"`
		Error  string            `json:"error,omitempty"`
	}

	var wg sync.WaitGroup
	results := make([]nodeResult, len(nodes))
	var mu sync.Mutex

	for i, node := range nodes {
		wg.Add(1)
		go func(idx int, addr string) {
			defer wg.Done()
			res := nodeResult{Addr: addr}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
			cancel()
			if err != nil {
				res.Error = "connect failed: " + err.Error()
				mu.Lock()
				results[idx] = res
				mu.Unlock()
				return
			}
			defer conn.Close()

			client := pb.NewKVServiceClient(conn)
			statusCtx, statusCancel := context.WithTimeout(context.Background(), 3*time.Second)
			statusResp, err := client.Status(statusCtx, &pb.StatusRequest{})
			statusCancel()
			if err != nil {
				res.Error = "status failed: " + err.Error()
			} else {
				res.Status = statusResp
			}

			mu.Lock()
			results[idx] = res
			mu.Unlock()
		}(i, node.Addr)
	}

	wg.Wait()

	var totalEntries, totalMemory int64
	var cpNodes, apNodes int
	for _, res := range results {
		if res.Status != nil {
			if res.Status.Stats != nil {
				totalEntries += res.Status.Stats.EntryCount
				totalMemory += res.Status.Stats.MemoryBytes
			}
			if res.Status.Mode == "cp" {
				cpNodes++
			} else {
				apNodes++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes":         results,
		"cp_nodes":      cpNodes,
		"ap_nodes":      apNodes,
		"total_entries": totalEntries,
		"total_memory":  totalMemory,
	})
}

// ResilienceStatus 返回限流器和熔断器状态
// GET /admin/resilience
func (h *Handler) ResilienceStatus(w http.ResponseWriter, r *http.Request) {
	var rlStatus map[string]interface{}
	if h.rateLimiter != nil {
		rlStatus = map[string]interface{}{
			"enabled": true,
			"type":    "token_bucket",
		}
	} else {
		rlStatus = map[string]interface{}{
			"enabled": false,
		}
	}

	var cbStatus map[string]interface{}
	if h.circuitBreaker != nil {
		cbStatus = map[string]interface{}{
			"enabled": true,
			"type":    "sliding_window",
			"state":   h.circuitBreaker.State().String(),
		}
	} else {
		cbStatus = map[string]interface{}{
			"enabled": false,
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"rate_limiter":    rlStatus,
		"circuit_breaker": cbStatus,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
