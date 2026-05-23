package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	pb "Flux-KV/api/proto"
	"Flux-KV/internal/app"
	"Flux-KV/internal/config"
	"Flux-KV/internal/raft"
	"Flux-KV/internal/storage"
	grpctransport "Flux-KV/internal/transport/grpc"
	"Flux-KV/pkg/network/discovery"
	"Flux-KV/pkg/network/tracer"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
)

func main() {
	var port int
	flag.IntVar(&port, "port", 0, "gRPC server port (overrides config)")
	flag.Parse()

	// 1. 初始化配置
	config.InitConfig()
	cfg := config.GetConfig()

	// 命令行端口优先级高于配置
	if port > 0 {
		cfg.Server.Port = port
	}

	config.PrintConfig()

	// 2. 初始化 Jaeger Tracer
	if cfg.Jaeger.Endpoint != "" {
		tp, err := tracer.InitTracer("kv-service", cfg.Jaeger.Endpoint)
		if err != nil {
			log.Printf("⚠️  Jaeger tracer init failed: %v", err)
		} else {
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = tp.Shutdown(ctx)
			}()
			log.Println("✅ Jaeger tracer initialized")
		}
	}

	// 3. 创建存储引擎
	engine, err := storage.NewEngine(cfg)
	if err != nil {
		log.Fatalf("❌ Failed to create storage engine: %v", err)
	}

	// 提前计算服务地址（Raft BindAddr 和 Etcd 注册都需要）
	serviceIP := config.GetServiceIP()
	serviceAddr := fmt.Sprintf("%s:%d", serviceIP, cfg.Server.Port)

	// 4. 根据模式包装存储层（CP / AP）
	var db storage.Engine
	var nodeMode string
	var isLeader bool
	var cpStore *storage.CPStorage

	if cfg.Raft.Enabled {
		nodeMode = "cp"
		raftCfg := &raft.Config{
			NodeID:   cfg.Raft.NodeID,
			GroupID:  cfg.Raft.GroupID,
			Peers:    cfg.Raft.Peers,
			BindAddr: serviceAddr,
			DataDir:  cfg.Raft.DataDir,
		}
		var err error
		cpStore, err = storage.NewCPStorage(engine, raftCfg)
		if err != nil {
			log.Fatalf("❌ Failed to init CP storage: %v", err)
		}
		defer cpStore.Close()
		db = cpStore
		isLeader = cpStore.IsLeader()
		log.Printf("✅ CP storage initialized (Raft node: %s, group: %s)", cfg.Raft.NodeID, cfg.Raft.GroupID)
	} else {
		nodeMode = "ap"
		apStore := storage.NewAPStorage(engine)
		defer apStore.Close()
		db = apStore
		log.Println("✅ AP storage initialized")
	}

	// 5. 创建应用层 UseCase
	nodeMeta := app.NodeMeta{
		NodeID: cfg.Raft.NodeID,
		Mode:   nodeMode,
	}
	if nodeMeta.NodeID == "" {
		nodeMeta.NodeID = config.GetServiceIP()
	}
	uc := app.NewKVUseCase(db, nodeMeta)

	// 6. 启动 gRPC 服务（KV + Raft 共享端口）
	grpcServer := grpc.NewServer()
	pb.RegisterKVServiceServer(grpcServer, grpctransport.NewKVServer(uc))
	if cpStore != nil {
		raft.RegisterRaftService(grpcServer, cpStore.Node())
		log.Println("✅ Raft service registered on main gRPC server")
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Server.Port))
	if err != nil {
		log.Fatalf("❌ Failed to listen gRPC on port %d: %v", cfg.Server.Port, err)
	}

	go func() {
		log.Printf("🚀 KV gRPC server listening on :%d", cfg.Server.Port)
		if err := grpcServer.Serve(lis); err != nil {
			log.Printf("gRPC server stopped: %v", err)
		}
	}()

	// 7. 启动 HTTP 管理服务器（Prometheus metrics + pprof）
	// 使用固定端口 9090 作为内部管理端口，与 docker-compose 映射保持一致
	adminMux := http.NewServeMux()
	adminMux.Handle("/metrics", promhttp.Handler())
	adminMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
			"node":   nodeMeta.NodeID,
			"mode":   nodeMode,
		})
	})

	adminServer := &http.Server{
		Addr:    ":9090",
		Handler: adminMux,
	}

	go func() {
		log.Println("🚀 Admin HTTP server listening on :9090")
		if err := adminServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Admin server stopped: %v", err)
		}
	}()

	// 8. 启动 pprof（如果启用）
	if cfg.Pprof.Enabled {
		go func() {
			addr := fmt.Sprintf(":%d", cfg.Pprof.Port)
			log.Printf("🚀 Pprof server listening on %s", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				log.Printf("Pprof server stopped: %v", err)
			}
		}()
	}

	// 9. 注册到 Etcd
	var registry *discovery.Registry
	var migrator *app.Migrator
	if len(cfg.Etcd.Endpoints) > 0 {
		registry, err = discovery.NewRegistry(cfg.Etcd.Endpoints)
		if err != nil {
			log.Printf("⚠️  Failed to connect Etcd: %v", err)
		} else {
			nodeInfo := discovery.NodeInfo{
				NodeID:     nodeMeta.NodeID,
				Addr:       serviceAddr,
				GroupID:    cfg.Raft.GroupID,
				Mode:       nodeMode,
				IsLeader:   isLeader,
				EngineType: cfg.Storage.ShardType + "-" + cfg.Storage.LockerType,
				RaftAddr:   serviceAddr,
				Metadata: map[string]string{
					"version": "v0.1.0",
				},
			}
			infoJSON, _ := nodeInfo.ToJSON()
			key := fmt.Sprintf("/services/kv-service/%s", serviceAddr)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := registry.Register(ctx, key, infoJSON, 10); err != nil {
				log.Printf("⚠️  Etcd register failed: %v", err)
			} else {
				log.Printf("✅ Service registered to Etcd: %s", key)
			}

			// AP 节点启动后台数据迁移器
			if nodeMode == "ap" {
				disco, err := discovery.NewDiscovery(cfg.Etcd.Endpoints)
				if err != nil {
					log.Printf("⚠️  Failed to create discovery for migrator: %v", err)
				} else {
					migrator = app.NewMigrator(db, disco, nodeMeta, serviceAddr)
					migrator.Start()
					log.Println("✅ Migrator started for AP node")
				}
			}
		}
	}

	// 注册 readiness probe
	adminMux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		ready := true
		var checks []string
		if registry != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := registry.Status(ctx); err != nil {
				ready = false
				checks = append(checks, "etcd: "+err.Error())
			}
		}
		if db == nil {
			ready = false
			checks = append(checks, "storage: not initialized")
		}
		w.Header().Set("Content-Type", "application/json")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":  "not_ready",
				"checks":  checks,
				"time":    time.Now().Format(time.RFC3339),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ready",
			"time":   time.Now().Format(time.RFC3339),
			"node":   nodeMeta.NodeID,
			"mode":   nodeMode,
		})
	})

	// 10. 优雅退出
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("\n🛑 Shutting down gracefully...")

	// 关闭 gRPC server
	grpcServer.GracefulStop()

	// 关闭 admin HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = adminServer.Shutdown(shutdownCtx)

	// 停止 Migrator
	if migrator != nil {
		migrator.Stop()
		log.Println("✅ Migrator stopped")
	}

	// 注销 Etcd
	if registry != nil {
		registry.Close()
		log.Println("✅ Etcd registry closed")
	}

	log.Println("✅ Server stopped")
}
